package rpc

import (
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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type PrimaryCollectorHandler struct {
	pb.UnimplementedSantaiziCollectorServiceServer
	pb.UnimplementedSantaiziReplicationServiceServer
	signer      *telemetryservice.Signer
	store       *telemetryservice.Store
	agentCA     *pki.Authority
	collectorCA *pki.Authority
}

func NewPrimaryCollectorHandler(bundle *pki.Bundle) (*PrimaryCollectorHandler, error) {
	signer, err := telemetryservice.LoadOrCreateSigner(singleton.Conf.Telemetry.SigningKeyPath)
	if err != nil {
		return nil, err
	}
	handler := &PrimaryCollectorHandler{
		signer: signer,
		store:  telemetryservice.NewStoreWithBucketSize(singleton.DB, time.Duration(singleton.Conf.Telemetry.AvailabilityBucketSeconds)*time.Second),
	}
	if bundle != nil {
		handler.agentCA = bundle.Agent
		handler.collectorCA = bundle.Collector
	}
	return handler, nil
}

func (h *PrimaryCollectorHandler) Register(ctx context.Context, request *pb.RegisterCollectorRequest) (*pb.RegisterCollectorResponse, error) {
	collector, err := findCollectorByToken(ctx, request.GetRegistrationToken())
	if err != nil {
		return nil, err
	}
	response := &pb.RegisterCollectorResponse{
		CollectorUuid: collector.CollectorUUID, PrimaryPublicKey: h.signer.PublicKey(), KeyId: h.signer.KeyID(),
		ConfigVersion: collector.ConfigVersion,
	}
	if h.agentCA != nil {
		response.AgentCaCertificatePem = string(h.agentCA.CertPEM)
	}
	if len(request.GetCsrDer()) > 0 {
		if h.collectorCA == nil {
			return nil, status.Error(codes.FailedPrecondition, "collector CA is not available")
		}
		certPEM, notBefore, notAfter, err := pki.SignCollectorCSR(h.collectorCA, request.GetCsrDer(), collector.CollectorUUID, time.Now())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		response.CollectorCertificatePem = string(certPEM)
		response.CollectorCaCertificatePem = string(h.collectorCA.CertPEM)
		response.NotBeforeUnix = notBefore.Unix()
		response.ExpiresAtUnix = notAfter.Unix()
	}
	return response, nil
}

func (h *PrimaryCollectorHandler) RenewCollector(ctx context.Context, request *pb.CollectorRenewRequest) (*pb.CollectorEnrollResponse, error) {
	ident, hasCert, err := pki.PeerDeviceIdentityFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if !hasCert || ident.Kind != pki.DeviceCollector {
		return nil, status.Error(codes.Unauthenticated, "collector certificate is required")
	}
	if ident.CollectorUUID != request.GetCollectorUuid() {
		return nil, status.Error(codes.PermissionDenied, "certificate UUID does not match request")
	}
	if _, err := findCollectorByUUID(ctx, ident.CollectorUUID); err != nil {
		return nil, err
	}
	if h.collectorCA == nil {
		return nil, status.Error(codes.FailedPrecondition, "collector CA is not available")
	}
	certPEM, notBefore, notAfter, err := pki.SignCollectorCSR(h.collectorCA, request.GetCsrDer(), ident.CollectorUUID, time.Now())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	response := &pb.CollectorEnrollResponse{
		CertificatePem: string(certPEM), CaCertificatePem: string(h.collectorCA.CertPEM),
		NotBeforeUnix: notBefore.Unix(), ExpiresAtUnix: notAfter.Unix(),
	}
	if h.agentCA != nil {
		response.AgentCaCertificatePem = string(h.agentCA.CertPEM)
	}
	return response, nil
}

func (h *PrimaryCollectorHandler) Sync(stream grpc.BidiStreamingServer[pb.CollectorSyncRequest, pb.CollectorSyncResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "collector sync hello is required")
	}
	collector, err := h.identifyCollector(stream.Context(), hello.GetCollectorUuid(), hello.GetRegistrationToken())
	if err != nil {
		return err
	}
	if err := h.sendCollectorConfig(stream, collector); err != nil {
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
		if samples := request.GetProbeSamples(); samples != nil {
			if err := telemetryservice.IngestProbeSamples(singleton.DB.WithContext(stream.Context()), collector, samples, time.Now()); err != nil {
				return err
			}
			if err := stream.Send(&pb.CollectorSyncResponse{Body: &pb.CollectorSyncResponse_Accepted{Accepted: true}}); err != nil {
				return err
			}
			continue
		}
		if runtime := request.GetRuntime(); runtime != nil {
			if err := saveCollectorRuntime(stream.Context(), collector.CollectorUUID, runtime, time.Now()); err != nil {
				return err
			}
			var current model.Collector
			if err := singleton.DB.WithContext(stream.Context()).First(&current, "collector_uuid = ?", collector.CollectorUUID).Error; err != nil {
				return err
			}
			if current.ConfigVersion != collector.ConfigVersion {
				collector = &current
				if err := h.sendCollectorConfig(stream, collector); err != nil {
					return err
				}
			} else if err := stream.Send(&pb.CollectorSyncResponse{Body: &pb.CollectorSyncResponse_Accepted{Accepted: true}}); err != nil {
				return err
			}
			if collector.IsProbe() {
				jobs, jobErr := telemetryservice.ListPendingProbeRouteJobs(singleton.DB.WithContext(stream.Context()), collector.CollectorUUID, time.Now())
				if jobErr != nil {
					return jobErr
				}
				if batch := telemetryservice.PendingJobsToProto(jobs); batch != nil {
					if err := stream.Send(&pb.CollectorSyncResponse{Body: &pb.CollectorSyncResponse_RouteJobs{RouteJobs: batch}}); err != nil {
						return err
					}
				}
			}
		}
	}
}

func (h *PrimaryCollectorHandler) sendCollectorConfig(stream grpc.BidiStreamingServer[pb.CollectorSyncRequest, pb.CollectorSyncResponse], collector *model.Collector) error {
	var rows []model.ObserverAssignment
	if err := singleton.DB.WithContext(stream.Context()).Where("observer_id = ?", collector.CollectorUUID).Find(&rows).Error; err != nil {
		return err
	}
	config := &pb.CollectorAuthorizationConfig{
		ConfigVersion: collector.ConfigVersion, PrimaryPublicKey: h.signer.PublicKey(), KeyId: h.signer.KeyID(),
		Kind: telemetryservice.ProtoCollectorKind(collector.Kind), Probe: telemetryservice.ProbeConfigFromCollector(collector),
	}
	if h.agentCA != nil {
		config.AgentCaCertificatePem = string(h.agentCA.CertPEM)
	}
	if collector.IsProbe() {
		targets, err := telemetryservice.BuildProbeTargets(singleton.DB.WithContext(stream.Context()), collector)
		if err != nil {
			return err
		}
		config.Targets = targets
	} else {
		for _, row := range rows {
			config.Assignments = append(config.Assignments, &pb.NodeAssignment{
				NodeUuid: row.NodeUUID, ObserverId: row.ObserverID, ValidFromUnixNano: row.ValidFrom,
				ValidToUnixNano: row.ValidTo, Generation: row.Generation, ConfigVersion: row.ConfigVersion,
			})
		}
	}
	return stream.Send(&pb.CollectorSyncResponse{Body: &pb.CollectorSyncResponse_Config{Config: config}})
}

func (h *PrimaryCollectorHandler) Replicate(stream grpc.BidiStreamingServer[pb.ReplicationBatch, pb.ReplicationAck]) error {
	collector, err := h.identifyCollector(stream.Context(), "", collectorTokenFromMetadata(stream.Context()))
	if err != nil {
		return err
	}
	for {
		batch, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if batch.GetCollectorUuid() != collector.CollectorUUID {
			return status.Error(codes.PermissionDenied, "replication collector identity mismatch")
		}
		committed, err := h.store.Replicate(stream.Context(), batch, time.Now())
		if err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		if err := stream.Send(&pb.ReplicationAck{
			CollectorUuid: collector.CollectorUUID, ReplicationSession: batch.GetReplicationSession(),
			BatchSequence: batch.GetBatchSequence(), CommittedSpoolThroughId: committed,
		}); err != nil {
			return err
		}
	}
}

func (h *PrimaryCollectorHandler) identifyCollector(ctx context.Context, collectorUUID, token string) (*model.Collector, error) {
	ident, hasCert, err := pki.PeerDeviceIdentityFromContext(ctx)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	if hasCert {
		if ident.Kind != pki.DeviceCollector {
			return nil, status.Error(codes.PermissionDenied, "collector certificate is required")
		}
		if collectorUUID != "" && ident.CollectorUUID != collectorUUID {
			return nil, status.Error(codes.PermissionDenied, "certificate UUID does not match hello")
		}
		return findCollectorByUUID(ctx, ident.CollectorUUID)
	}
	collector, err := findCollectorByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if collectorUUID != "" && collector.CollectorUUID != collectorUUID {
		return nil, status.Error(codes.PermissionDenied, "collector token identity mismatch")
	}
	return collector, nil
}

func findCollectorByUUID(ctx context.Context, collectorUUID string) (*model.Collector, error) {
	if collectorUUID == "" {
		return nil, status.Error(codes.Unauthenticated, "collector UUID is required")
	}
	var collector model.Collector
	if err := singleton.DB.WithContext(ctx).Where("collector_uuid = ? AND revoked = ? AND deleted = ?", collectorUUID, false, false).First(&collector).Error; err != nil {
		return nil, status.Error(codes.Unauthenticated, "collector certificate is invalid or revoked")
	}
	return &collector, nil
}

func findCollectorByToken(ctx context.Context, token string) (*model.Collector, error) {
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, "collector token is required")
	}
	var collectors []model.Collector
	if err := singleton.DB.WithContext(ctx).Where("revoked = ? AND deleted = ?", false, false).Find(&collectors).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	for index := range collectors {
		if telemetryservice.RegistrationTokenMatches(token, collectors[index].TokenHash) {
			return &collectors[index], nil
		}
	}
	return nil, status.Error(codes.Unauthenticated, "collector token is invalid or revoked")
}

func collectorTokenFromMetadata(ctx context.Context) string {
	values, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	tokens := values.Get("collector_token")
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

func saveCollectorRuntime(ctx context.Context, collectorUUID string, runtime *pb.CollectorRuntime, now time.Time) error {
	row := telemetryservice.CollectorRuntimeFromProto(collectorUUID, runtime, now, false)
	return telemetryservice.UpsertCollectorRuntime(singleton.DB.WithContext(ctx), row, false)
}
