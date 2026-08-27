package rpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/pki"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	telemetryservice "github.com/hi2shark/santaizi-dashboard/service/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm/clause"
)

type V2Handler struct {
	pb.UnimplementedSantaiziTelemetryServiceServer
	pb.UnimplementedSantaiziControlServiceServer
	pb.UnimplementedSantaiziNATServiceServer
	signer      *telemetryservice.Signer
	store       *telemetryservice.Store
	auth        *authHandler
	ingestQueue chan primaryIngestJob
}

type primaryIngestJob struct {
	ctx        context.Context
	batch      *pb.TelemetryBatch
	receivedAt time.Time
	result     chan primaryIngestResult
}

type primaryIngestResult struct {
	value *telemetryservice.IngestResult
	err   error
}

func NewV2Handler() (*V2Handler, error) {
	signer, err := telemetryservice.LoadOrCreateSigner(singleton.Conf.Telemetry.SigningKeyPath)
	if err != nil {
		return nil, err
	}
	queueSize := singleton.Conf.Telemetry.IngestQueueSize
	if queueSize <= 0 {
		queueSize = 4096
	}
	handler := &V2Handler{
		signer: signer, store: telemetryservice.NewStoreWithBucketSize(singleton.DB, time.Duration(singleton.Conf.Telemetry.AvailabilityBucketSeconds)*time.Second), auth: &authHandler{},
		ingestQueue: make(chan primaryIngestJob, queueSize),
	}
	go handler.runPrimaryIngest()
	return handler, nil
}

func (h *V2Handler) runPrimaryIngest() {
	for job := range h.ingestQueue {
		value, err := h.store.Ingest(job.ctx, job.batch, "primary", job.receivedAt)
		job.result <- primaryIngestResult{value: value, err: err}
	}
}

func (h *V2Handler) enqueuePrimaryIngest(ctx context.Context, batch *pb.TelemetryBatch, receivedAt time.Time) (*telemetryservice.IngestResult, error) {
	if batch == nil || len(batch.GetRecords()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "telemetry batch is empty")
	}
	if limit := singleton.Conf.Telemetry.IngestBatchSize; limit > 0 && len(batch.GetRecords()) > limit {
		return nil, status.Errorf(codes.ResourceExhausted, "telemetry batch exceeds limit %d", limit)
	}
	job := primaryIngestJob{ctx: ctx, batch: batch, receivedAt: receivedAt, result: make(chan primaryIngestResult, 1)}
	select {
	case h.ingestQueue <- job:
	default:
		return nil, status.Error(codes.ResourceExhausted, "primary telemetry ingest queue is full")
	}
	select {
	case result := <-job.result:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *V2Handler) Ingest(stream grpc.BidiStreamingServer[pb.TelemetryRequest, pb.TelemetryResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil || hello.GetEndpointId() != "primary" {
		return status.Error(codes.InvalidArgument, "primary telemetry hello is required")
	}
	verification, err := telemetryservice.VerifyCredential(
		h.signer.PublicKey(), h.signer.KeyID(), hello.GetCredential(), time.Now(),
		0, false,
	)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if !bytes.Equal(verification.Claims.GetNodeUuid(), hello.GetNodeUuid()) {
		return status.Error(codes.PermissionDenied, "credential node does not match telemetry hello")
	}
	if err := matchAgentCertificate(stream.Context(), hello.GetNodeUuid(), verification.Claims.GetNodeUuid()); err != nil {
		return err
	}

	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		switch body := request.GetBody().(type) {
		case *pb.TelemetryRequest_Batch:
			result, err := h.enqueuePrimaryIngest(stream.Context(), body.Batch, now)
			if err != nil {
				if status.Code(err) != codes.Unknown {
					return err
				}
				return status.Error(codes.InvalidArgument, err.Error())
			}
			for _, event := range result.FreshEvents {
				if err := singleton.ApplyV2Event(event, now); err != nil {
					return status.Error(codes.Internal, err.Error())
				}
			}
			if err := stream.Send(&pb.TelemetryResponse{Acks: result.Acks}); err != nil {
				return err
			}
		case *pb.TelemetryRequest_RealtimeSnapshot:
			if !bytes.Equal(body.RealtimeSnapshot.GetNodeUuid(), hello.GetNodeUuid()) {
				return status.Error(codes.PermissionDenied, "realtime snapshot node mismatch")
			}
			if err := singleton.ApplyRealtimeSnapshot(body.RealtimeSnapshot, now); err != nil {
				return status.Error(codes.InvalidArgument, err.Error())
			}
		case *pb.TelemetryRequest_Ping:
			if err := stream.Send(&pb.TelemetryResponse{Pong: &pb.TelemetryPong{}}); err != nil {
				return err
			}
		default:
			return status.Error(codes.InvalidArgument, "unexpected telemetry request")
		}
	}
}

func (h *V2Handler) Control(stream grpc.BidiStreamingServer[pb.AgentControlRequest, pb.PrimaryControlResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil || len(hello.GetNodeUuid()) != 16 || len(hello.GetSessionId()) != 16 {
		return status.Error(codes.InvalidArgument, "valid control hello is required")
	}
	serverID, err := h.authenticateControl(stream.Context(), hello)
	if err != nil {
		return err
	}
	if _, err := singleton.BindServerNodeForProtocol(serverID, hello.GetNodeUuid(), time.Now(), pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	session := newControlSession(serverID, hello, stream)
	registerControlSession(session)
	defer unregisterControlSession(session)
	conf := singleton.Conf
	if conf == nil {
		return status.Error(codes.Internal, "config is not initialized")
	}
	configVersion := singleton.CurrentTelemetryConfigVersion()
	credential, err := h.signer.Sign(
		hello.GetNodeUuid(), configVersion, time.Now(),
		time.Duration(conf.Telemetry.CredentialValidityDays)*24*time.Hour,
	)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if err := session.send(&pb.PrimaryControlResponse{Body: &pb.PrimaryControlResponse_Credential{Credential: credential}}); err != nil {
		return err
	}
	assignment, err := singleton.EndpointAssignmentForNode(hello.GetNodeUuid(), hello.GetSessionId(), 1)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if err := session.send(&pb.PrimaryControlResponse{Body: &pb.PrimaryControlResponse_Assignment{Assignment: assignment}}); err != nil {
		return err
	}

	requests := make(chan *pb.AgentControlRequest, 1)
	recvErr := make(chan error, 1)
	go func() {
		for {
			request, err := stream.Recv()
			if err != nil {
				select {
				case recvErr <- err:
				case <-stream.Context().Done():
				}
				return
			}
			select {
			case requests <- request:
			case <-stream.Context().Done():
				return
			}
		}
	}()
	assignmentTicker := time.NewTicker(15 * time.Second)
	defer assignmentTicker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case err := <-recvErr:
			return err
		case request := <-requests:
			switch body := request.GetBody().(type) {
			case *pb.AgentControlRequest_ProbeResult:
				dispatchProbeResult(serverID, body.ProbeResult)
			case *pb.AgentControlRequest_NatOpenResult:
				resolveNATOpenResult(serverID, body.NatOpenResult)
			case *pb.AgentControlRequest_Runtime:
				sinkPayload, _ := proto.Marshal(body.Runtime)
				row := model.AgentTelemetryRuntime{
					NodeUUID: hello.GetNodeUuid(), WalPressure: int32(body.Runtime.GetWalPressure()),
					WalBytes: body.Runtime.GetWalBytes(), PendingEvents: body.Runtime.GetPendingEvents(),
					OldestPending: body.Runtime.GetOldestPendingUnixNano(), SinkCursors: sinkPayload,
					ClockUntrusted: body.Runtime.GetClockUntrusted(), ProtocolVersion: body.Runtime.GetProtocolVersion(),
				}
				if err := singleton.DB.Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "node_uuid"}}, UpdateAll: true,
				}).Create(&row).Error; err != nil {
					return status.Error(codes.Internal, err.Error())
				}
				if err := telemetryservice.RecordAgentSinkLatency(singleton.DB.WithContext(stream.Context()), hello.GetNodeUuid(), body.Runtime); err != nil {
					return status.Error(codes.Internal, err.Error())
				}
			}
		case <-assignmentTicker.C:
			activation := uint64(1)
			var runtime model.ServerRuntime
			if err := singleton.DB.Select("current_sequence").First(&runtime, "server_id = ?", serverID).Error; err == nil {
				activation = runtime.CurrentSequence + 1
			}
			assignment, err := singleton.EndpointAssignmentForNode(hello.GetNodeUuid(), hello.GetSessionId(), activation)
			if err != nil {
				return status.Error(codes.Internal, err.Error())
			}
			if err := session.send(&pb.PrimaryControlResponse{Body: &pb.PrimaryControlResponse_Assignment{Assignment: assignment}}); err != nil {
				return err
			}
		}
	}
}

func (h *V2Handler) authenticateControl(ctx context.Context, hello *pb.AgentControlHello) (uint64, error) {
	ident, hasCert, err := pki.PeerDeviceIdentityFromContext(ctx)
	if err != nil {
		return 0, status.Error(codes.PermissionDenied, err.Error())
	}
	if hasCert {
		if ident.Kind != pki.DeviceAgent {
			return 0, status.Error(codes.PermissionDenied, "control requires an agent certificate")
		}
		if !bytes.Equal(ident.NodeUUID, hello.GetNodeUuid()) {
			return 0, status.Error(codes.PermissionDenied, "certificate UUID does not match hello")
		}
		serverID, err := singleton.ServerIDFromNodeUUID(ident.NodeUUID)
		if err != nil {
			return 0, status.Error(codes.Unauthenticated, "node is not bound to a server")
		}
		return serverID, nil
	}
	return h.auth.Check(ctx)
}

func (h *V2Handler) authenticateAgent(ctx context.Context) (uint64, error) {
	ident, hasCert, err := pki.PeerDeviceIdentityFromContext(ctx)
	if err != nil {
		return 0, status.Error(codes.PermissionDenied, err.Error())
	}
	if hasCert {
		if ident.Kind != pki.DeviceAgent {
			return 0, status.Error(codes.PermissionDenied, "agent certificate is required")
		}
		serverID, err := singleton.ServerIDFromNodeUUID(ident.NodeUUID)
		if err != nil {
			return 0, status.Error(codes.Unauthenticated, "node is not bound to a server")
		}
		return serverID, nil
	}
	return h.auth.Check(ctx)
}

func matchAgentCertificate(ctx context.Context, helloUUID, credentialUUID []byte) error {
	ident, hasCert, err := pki.PeerDeviceIdentityFromContext(ctx)
	if err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	if !hasCert {
		if singleton.Conf != nil && singleton.Conf.GRPCTLS.RequireAgentMTLS {
			return status.Error(codes.Unauthenticated, "agent certificate is required")
		}
		return nil
	}
	if ident.Kind != pki.DeviceAgent {
		return status.Error(codes.PermissionDenied, "ingest requires an agent certificate")
	}
	if !bytes.Equal(ident.NodeUUID, helloUUID) || !bytes.Equal(ident.NodeUUID, credentialUUID) {
		return status.Error(codes.PermissionDenied, "certificate UUID does not match hello or credential")
	}
	return nil
}
