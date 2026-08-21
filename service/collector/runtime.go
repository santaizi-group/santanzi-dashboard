package collector

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/pki"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"github.com/hi2shark/santaizi-dashboard/service/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const collectorProtocolVersion = "2"

const (
	replicationBatchSize = 512
	replicationIdleWait  = 2 * time.Second
	replicationRetryMin  = time.Second
	replicationRetryMax  = 10 * time.Second
)

type Runtime struct {
	pb.UnimplementedSantaiziTelemetryServiceServer
	pb.UnimplementedSantaiziCollectorServiceServer

	store  *Store
	config model.CollectorModeConfig
	grace  time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu                 sync.RWMutex
	collectorUUID      string
	processSession     string
	replicationSession []byte
	nextBatchSequence  uint64
	lastReplicationAck uint64
	syncSentAt         time.Time
	heartbeatRttMs     float64
	heartbeatRttAt     int64
	replicationRttMs   float64
	replicationRttAt   int64
	connectedAgents    atomic.Uint64
	replicateWake      chan struct{}
	clientStore        *pki.ClientStore
	forceAgentIngest   bool
	renewBackoff       time.Duration
	nextRenew          time.Time
	legacyPrimary      bool
	kind               string
	probeMu            sync.Mutex
	pendingSamples     []*pb.ProbeSample
	routeCh            chan routeWork
	routeInflight      map[string]struct{}
}

func NewRuntime(parent context.Context, store *Store, config model.CollectorModeConfig, grace time.Duration) (*Runtime, error) {
	if store == nil || config.PrimaryEndpoint == "" || config.RegistrationToken == "" {
		return nil, errors.New("collector primary endpoint and registration token are required")
	}
	processID := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, processID); err != nil {
		return nil, err
	}
	replicationID := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, replicationID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	runtime := &Runtime{
		store: store, config: config, grace: grace, ctx: ctx, cancel: cancel,
		processSession: hex.EncodeToString(processID), replicationSession: replicationID, nextBatchSequence: 1,
		replicateWake: make(chan struct{}, 1), routeCh: make(chan routeWork, 32),
	}
	pkiDir := filepath.Join(filepath.Dir(config.DatabasePath), "pki")
	clientStore, err := pki.NewClientStore(pkiDir)
	if err != nil {
		cancel()
		return nil, err
	}
	runtime.clientStore = clientStore
	if cache, err := store.Authorization(ctx); err == nil {
		runtime.collectorUUID = cache.CollectorUUID
		runtime.kind = model.NormalizeCollectorKind(cache.Kind)
		if pem := cache.AgentCACertificatePEM; pem != "" {
			_ = clientStore.SaveAgentCA([]byte(pem))
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		cancel()
		return nil, err
	}
	return runtime, nil
}

func (r *Runtime) SetForceAgentIngest(enabled bool) {
	r.forceAgentIngest = enabled
}

func (r *Runtime) AgentCAPool() *x509.CertPool {
	pool := x509.NewCertPool()
	if r.clientStore == nil {
		return pool
	}
	if pemBytes, err := r.clientStore.LoadAgentCA(); err == nil && len(pemBytes) > 0 {
		_ = pki.AppendPEMToPool(pool, pemBytes)
	}
	if cache, err := r.store.Authorization(r.ctx); err == nil && cache.AgentCACertificatePEM != "" {
		_ = pki.AppendPEMToPool(pool, []byte(cache.AgentCACertificatePEM))
	}
	return pool
}

func (r *Runtime) Start() {
	r.wg.Add(5)
	go r.syncLoop()
	go r.replicationLoop()
	go r.healthLoop()
	go r.probeLoop()
	go r.routeWorker()
}

func (r *Runtime) Close() {
	r.cancel()
	r.wg.Wait()
}

func (r *Runtime) syncLoop() {
	defer r.wg.Done()
	for {
		if r.ctx.Err() != nil {
			return
		}
		if err := r.syncOnce(); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Printf("SANTAIZI>> collector sync disconnected: %v\n", err)
		}
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (r *Runtime) syncOnce() error {
	conn, err := grpc.NewClient(r.config.PrimaryEndpoint, r.dialOptions()...)
	if err != nil {
		return err
	}
	defer conn.Close()
	client := pb.NewSantaiziCollectorServiceClient(conn)
	if err := r.ensureCollectorCertificate(client); err != nil {
		return err
	}
	r.mu.RLock()
	collectorUUID := r.collectorUUID
	r.mu.RUnlock()
	if collectorUUID == "" || (!r.hasCollectorCert() && !r.legacyPrimary) {
		if err := r.registerWithPrimary(client); err != nil {
			return err
		}
		r.mu.RLock()
		collectorUUID = r.collectorUUID
		r.mu.RUnlock()
	}
	stream, err := client.Sync(r.ctx)
	if err != nil {
		return err
	}
	cache, err := r.store.Authorization(r.ctx)
	if err != nil {
		return err
	}
	hello := &pb.CollectorSyncHello{
		CollectorUuid: collectorUUID, CurrentConfigVersion: cache.ConfigVersion, Runtime: r.runtimeSnapshot(),
	}
	if r.legacyPrimary || !r.hasCollectorCert() {
		hello.RegistrationToken = r.config.RegistrationToken
	}
	sent := time.Now()
	if err := stream.Send(&pb.CollectorSyncRequest{Body: &pb.CollectorSyncRequest_Hello{Hello: hello}}); err != nil {
		return err
	}
	r.markSyncSent(sent)
	recv := make(chan *pb.CollectorSyncResponse)
	recvErr := make(chan error, 1)
	go func() {
		for {
			response, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			select {
			case recv <- response:
			case <-r.ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		case err := <-recvErr:
			return err
		case response := <-recv:
			r.noteSyncRTT(time.Now())
			if config := response.GetConfig(); config != nil {
				if err := r.store.SaveAuthorization(r.ctx, collectorUUID, config, time.Now()); err != nil {
					return err
				}
				if config.GetKind() == pb.CollectorKind_COLLECTOR_KIND_PROBE {
					r.setKind(model.CollectorKindProbe)
				} else {
					r.setKind(model.CollectorKindObserver)
				}
				if pem := config.GetAgentCaCertificatePem(); pem != "" {
					_ = r.clientStore.SaveAgentCA([]byte(pem))
				}
			} else if jobs := response.GetRouteJobs(); jobs != nil {
				r.enqueueRouteJobs(jobs)
			} else if response.GetAccepted() {
				if err := r.store.TouchPrimarySeen(r.ctx, time.Now()); err != nil {
					return err
				}
			}
		case <-ticker.C:
			sent := time.Now()
			if err := stream.Send(&pb.CollectorSyncRequest{Body: &pb.CollectorSyncRequest_Runtime{Runtime: r.runtimeSnapshot()}}); err != nil {
				return err
			}
			r.markSyncSent(sent)
			if samples := r.takeProbeSamples(); samples != nil {
				if err := stream.Send(&pb.CollectorSyncRequest{Body: &pb.CollectorSyncRequest_ProbeSamples{ProbeSamples: samples}}); err != nil {
					return err
				}
			}
		}
	}
}

func (r *Runtime) replicationLoop() {
	defer r.wg.Done()
	retry := replicationRetryMin
	for {
		if r.ctx.Err() != nil {
			return
		}
		if r.isProbe() {
			select {
			case <-r.ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
			continue
		}
		err := r.replicationOnce()
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Printf("SANTAIZI>> collector replication disconnected: %v\n", err)
		}
		if r.ctx.Err() != nil {
			return
		}
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(retry):
		}
		retry = nextReplicationRetry(retry)
		if err == nil {
			retry = replicationRetryMin
		}
	}
}

type replicationStream interface {
	Send(*pb.ReplicationBatch) error
	Recv() (*pb.ReplicationAck, error)
}

func (r *Runtime) replicationOnce() error {
	r.mu.RLock()
	collectorUUID := r.collectorUUID
	r.mu.RUnlock()
	if collectorUUID == "" {
		return errors.New("collector is not registered")
	}
	conn, err := grpc.NewClient(r.config.PrimaryEndpoint, r.replicationDialOptions()...)
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := pb.NewSantaiziReplicationServiceClient(conn).Replicate(r.ctx)
	if err != nil {
		return err
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		flushed, err := r.flushOutbox(stream)
		if err != nil {
			return err
		}
		if flushed > 0 {
			continue
		}
		if r.replicateWake == nil {
			select {
			case <-r.ctx.Done():
				return r.ctx.Err()
			case <-time.After(replicationIdleWait):
			}
			continue
		}
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		case <-r.replicateWake:
		case <-time.After(replicationIdleWait):
		}
	}
}

func (r *Runtime) flushOutbox(stream replicationStream) (int, error) {
	flushed := 0
	for {
		sent, err := r.replicateBatch(stream)
		if err != nil {
			return flushed, err
		}
		if !sent {
			return flushed, nil
		}
		flushed++
	}
}

func (r *Runtime) replicateBatch(stream replicationStream) (bool, error) {
	r.mu.RLock()
	collectorUUID := r.collectorUUID
	session := append([]byte(nil), r.replicationSession...)
	batchSequence := r.nextBatchSequence
	r.mu.RUnlock()
	outbox, err := r.store.ReadOutbox(r.ctx, replicationBatchSize)
	if err != nil {
		return false, err
	}
	if outbox.Through == 0 {
		return false, nil
	}
	batch := &pb.ReplicationBatch{
		CollectorUuid: collectorUUID, ReplicationSession: session, BatchSequence: batchSequence,
		SpoolThroughId: outbox.Through, Events: outbox.Events, Observations: outbox.Observations,
		Gaps: outbox.Gaps, Health: outbox.Health, Runtime: r.runtimeSnapshot(), DataLoss: outbox.DataLoss,
	}
	sent := time.Now()
	if err := stream.Send(batch); err != nil {
		return false, err
	}
	ack, err := stream.Recv()
	if err != nil {
		return false, err
	}
	if ack.GetError() != "" {
		return false, errors.New(ack.GetError())
	}
	if ack.GetCollectorUuid() != collectorUUID || !subtleBytesEqual(ack.GetReplicationSession(), session) || ack.GetBatchSequence() != batchSequence {
		return false, errors.New("replication ACK identity mismatch")
	}
	if err := r.store.CommitReplicationAck(r.ctx, ack.GetCommittedSpoolThroughId()); err != nil {
		return false, err
	}
	r.noteReplicationRTT(sent, time.Now())
	r.mu.Lock()
	r.lastReplicationAck = ack.GetCommittedSpoolThroughId()
	r.nextBatchSequence++
	r.mu.Unlock()
	return true, nil
}

func (r *Runtime) wakeReplication() {
	if r == nil || r.replicateWake == nil {
		return
	}
	select {
	case r.replicateWake <- struct{}{}:
	default:
	}
}

func nextReplicationRetry(current time.Duration) time.Duration {
	switch {
	case current < 2*time.Second:
		return 2 * time.Second
	case current < 5*time.Second:
		return 5 * time.Second
	default:
		return replicationRetryMax
	}
}

func (r *Runtime) healthLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case sampledAt := <-ticker.C:
			r.mu.RLock()
			observerID := r.collectorUUID
			r.mu.RUnlock()
			if observerID == "" {
				continue
			}
			if err := r.store.RecordHealth(r.ctx, &pb.ObserverHealthSample{
				ObserverId: observerID, SampledAtUnixNano: sampledAt.UnixNano(), Healthy: true, ProcessSession: r.processSession,
			}); err != nil {
				fmt.Printf("SANTAIZI>> record collector health: %v\n", err)
			}
			if err := r.store.EnforceSpoolPolicy(r.ctx, observerID, r.config.SpoolMaxBytes,
				time.Duration(r.config.SpoolMaxAgeDays)*24*time.Hour, sampledAt); err != nil {
				fmt.Printf("SANTAIZI>> enforce collector spool policy: %v\n", err)
			}
		}
	}
}

func (r *Runtime) Ingest(stream grpc.BidiStreamingServer[pb.TelemetryRequest, pb.TelemetryResponse]) error {
	if r.isProbe() {
		return status.Error(codes.FailedPrecondition, "probe collector does not accept agent ingest")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil || len(hello.GetNodeUuid()) != 16 {
		return status.Error(codes.InvalidArgument, "collector telemetry hello is required")
	}
	r.mu.RLock()
	collectorUUID := r.collectorUUID
	r.mu.RUnlock()
	if collectorUUID == "" || hello.GetEndpointId() != collectorUUID {
		return status.Error(codes.PermissionDenied, "telemetry endpoint identity mismatch")
	}
	authorized, err := r.store.IsNodeAuthorized(stream.Context(), hello.GetNodeUuid(), time.Now())
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if !authorized {
		return status.Error(codes.PermissionDenied, "node is not assigned to this collector")
	}
	cache, err := r.store.Authorization(stream.Context())
	if err != nil {
		return status.Error(codes.Unavailable, "collector authorization cache is unavailable")
	}
	verification, err := telemetry.VerifyCredential(cache.PrimaryPublicKey, cache.KeyID, hello.GetCredential(), time.Now(), r.grace, authorized)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if !subtleBytesEqual(verification.Claims.GetNodeUuid(), hello.GetNodeUuid()) {
		return status.Error(codes.PermissionDenied, "credential node mismatch")
	}
	if err := r.matchIngestCertificate(stream.Context(), hello.GetNodeUuid(), verification.Claims.GetNodeUuid()); err != nil {
		return err
	}
	r.connectedAgents.Add(1)
	defer r.connectedAgents.Add(^uint64(0))
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if batch := request.GetBatch(); batch != nil {
			result, err := r.store.Ingest(stream.Context(), batch, collectorUUID, time.Now())
			if err != nil {
				return status.Error(codes.InvalidArgument, err.Error())
			}
			if result.Enqueued > 0 {
				r.wakeReplication()
			}
			if err := stream.Send(&pb.TelemetryResponse{Acks: result.Acks}); err != nil {
				return err
			}
			continue
		}
		if request.GetPing() != nil {
			if err := stream.Send(&pb.TelemetryResponse{Pong: &pb.TelemetryPong{}}); err != nil {
				return err
			}
		}
		// Realtime snapshots are deliberately not ACKed and do not advance the
		// reliable cursor. The Agent keeps a direct Primary realtime path.
	}
}

func (r *Runtime) GetStatus(ctx context.Context, request *pb.CollectorStatusRequest) (*pb.CollectorStatus, error) {
	if r.config.StatusAuthorization == "" || subtle.ConstantTimeCompare([]byte(request.GetAuthorization()), []byte(r.config.StatusAuthorization)) != 1 {
		return nil, status.Error(codes.Unauthenticated, "collector status authorization failed")
	}
	runtime := r.runtimeSnapshot()
	return &pb.CollectorStatus{
		ConnectedAgents: runtime.GetConnectedAgents(), SpoolSize: runtime.GetSpoolSize(), PendingRecords: runtime.GetPendingRecords(),
		OldestPendingUnixNano: runtime.GetOldestPendingUnixNano(), ReplicationCursor: runtime.GetReplicationCursor(),
		LastPrimarySeenUnixNano: runtime.GetLastPrimarySeenUnixNano(), ProtocolVersion: runtime.GetProtocolVersion(),
	}, nil
}

func (r *Runtime) runtimeSnapshot() *pb.CollectorRuntime {
	stats, _ := r.store.RuntimeStats(r.ctx)
	cache, _ := r.store.Authorization(r.ctx)
	r.mu.RLock()
	collectorUUID := r.collectorUUID
	replicationCursor := r.lastReplicationAck
	heartbeatRttMs := r.heartbeatRttMs
	heartbeatRttAt := r.heartbeatRttAt
	replicationRttMs := r.replicationRttMs
	replicationRttAt := r.replicationRttAt
	r.mu.RUnlock()
	runtime := &pb.CollectorRuntime{
		CollectorUuid: collectorUUID, SampledAtUnixNano: time.Now().UnixNano(), SpoolSize: stats.SpoolBytes,
		PendingRecords: stats.Pending, OldestPendingUnixNano: stats.OldestPending, ReplicationCursor: replicationCursor,
		ConnectedAgents: r.connectedAgents.Load(), ProtocolVersion: collectorProtocolVersion, SoftwareVersion: singleton.Version,
		HeartbeatRttMs: heartbeatRttMs, HeartbeatRttSampledAtUnixNano: heartbeatRttAt,
		ReplicationRttMs: replicationRttMs, ReplicationRttSampledAtUnixNano: replicationRttAt,
	}
	if cache != nil {
		runtime.LastPrimarySeenUnixNano = cache.LastPrimarySeenNano
	}
	return runtime
}

func (r *Runtime) markSyncSent(at time.Time) {
	r.mu.Lock()
	r.syncSentAt = at
	r.mu.Unlock()
}

func (r *Runtime) noteSyncRTT(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.syncSentAt.IsZero() {
		return
	}
	r.heartbeatRttMs = durationMilliseconds(now.Sub(r.syncSentAt))
	r.heartbeatRttAt = now.UnixNano()
	r.syncSentAt = time.Time{}
}

func (r *Runtime) noteReplicationRTT(started, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replicationRttMs = durationMilliseconds(now.Sub(started))
	r.replicationRttAt = now.UnixNano()
}

func durationMilliseconds(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return float64(d) / float64(time.Millisecond)
}

func (r *Runtime) dialOptions() []grpc.DialOption {
	return r.dialOptionsWithToken(false)
}

func (r *Runtime) replicationDialOptions() []grpc.DialOption {
	return r.dialOptionsWithToken(!r.hasCollectorCert() || r.legacyPrimary)
}

func (r *Runtime) dialOptionsWithToken(withToken bool) []grpc.DialOption {
	options := []grpc.DialOption{r.transportCredentials()}
	if withToken {
		if r.config.PrimaryTLS {
			options = append(options, grpc.WithPerRPCCredentials(&collectorBootstrapCredential{token: r.config.RegistrationToken}))
		} else {
			options = append(options, grpc.WithPerRPCCredentials(&collectorTokenCredential{token: r.config.RegistrationToken}))
		}
	}
	return options
}

func (r *Runtime) transportCredentials() grpc.DialOption {
	if !r.config.PrimaryTLS {
		return grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	cfg, err := pki.ClientTLSConfig(pki.ClientTLSOptions{
		InsecureSkipVerify:   r.config.PrimaryInsecureTLS,
		GetClientCertificate: r.clientStore.GetClientCertificate,
	})
	if err != nil || cfg == nil {
		cfg = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: r.config.PrimaryInsecureTLS}
	}
	if bundle, loadErr := r.clientStore.Load(); loadErr == nil && len(bundle.CAPEM) > 0 {
		if cfg.RootCAs == nil {
			cfg.RootCAs = x509.NewCertPool()
		}
		_ = pki.AppendPEMToPool(cfg.RootCAs, bundle.CAPEM)
	}
	return grpc.WithTransportCredentials(credentials.NewTLS(cfg))
}

func (r *Runtime) hasCollectorCert() bool {
	if r.clientStore == nil {
		return false
	}
	bundle, err := r.clientStore.Load()
	return err == nil && bundle != nil && !bundle.Expired(time.Now())
}

func (r *Runtime) registerWithPrimary(client pb.SantaiziCollectorServiceClient) error {
	key, err := pki.GenerateKey()
	if err != nil {
		return err
	}
	csr, err := pki.CreateCSR(key, pki.EncodeCollectorURI("pending"))
	if err != nil {
		return err
	}
	response, err := client.Register(r.ctx, &pb.RegisterCollectorRequest{
		RegistrationToken: r.config.RegistrationToken, ProtocolVersion: collectorProtocolVersion, CsrDer: csr,
	})
	if err != nil {
		return err
	}
	collectorUUID := response.GetCollectorUuid()
	config := &pb.CollectorAuthorizationConfig{
		ConfigVersion: response.GetConfigVersion(), PrimaryPublicKey: response.GetPrimaryPublicKey(), KeyId: response.GetKeyId(),
		AgentCaCertificatePem: response.GetAgentCaCertificatePem(),
	}
	if err := r.store.SaveAuthorization(r.ctx, collectorUUID, config, time.Now()); err != nil {
		return err
	}
	if pem := response.GetAgentCaCertificatePem(); pem != "" {
		_ = r.clientStore.SaveAgentCA([]byte(pem))
	}
	if certPEM := response.GetCollectorCertificatePem(); certPEM != "" {
		cert, err := pki.ParseCertificatePEM([]byte(certPEM))
		if err != nil {
			return err
		}
		if err := r.clientStore.Save(&pki.ClientBundle{
			Key: key, Cert: cert, CertPEM: []byte(certPEM), CAPEM: []byte(response.GetCollectorCaCertificatePem()),
		}); err != nil {
			return err
		}
		r.legacyPrimary = false
	} else {
		r.legacyPrimary = true
	}
	r.mu.Lock()
	r.collectorUUID = collectorUUID
	r.mu.Unlock()
	return nil
}

func (r *Runtime) ensureCollectorCertificate(client pb.SantaiziCollectorServiceClient) error {
	if r.legacyPrimary {
		return nil
	}
	bundle, err := r.clientStore.Load()
	if errors.Is(err, pki.ErrClientBundleNotFound) || (err == nil && bundle.Expired(time.Now()) && r.config.RegistrationToken != "") {
		return nil
	}
	if err != nil && !errors.Is(err, pki.ErrClientBundleNotFound) {
		return err
	}
	if bundle == nil {
		return nil
	}
	now := time.Now()
	if !bundle.NeedsRenew(now, pki.DefaultRenewWindow) || now.Before(r.nextRenew) {
		return nil
	}
	key, err := pki.GenerateKey()
	if err != nil {
		return err
	}
	r.mu.RLock()
	collectorUUID := r.collectorUUID
	r.mu.RUnlock()
	csr, err := pki.CreateCSR(key, pki.EncodeCollectorURI(collectorUUID))
	if err != nil {
		return err
	}
	response, err := client.RenewCollector(r.ctx, &pb.CollectorRenewRequest{CollectorUuid: collectorUUID, CsrDer: csr})
	if err != nil {
		if !bundle.Expired(now) {
			if r.renewBackoff == 0 {
				r.renewBackoff = time.Second
			} else if r.renewBackoff < 10*time.Second {
				r.renewBackoff *= 2
			}
			r.nextRenew = now.Add(r.renewBackoff)
			fmt.Printf("SANTAIZI>> collector certificate renew failed, keeping existing cert: %v\n", err)
			return nil
		}
		return err
	}
	cert, err := pki.ParseCertificatePEM([]byte(response.GetCertificatePem()))
	if err != nil {
		return err
	}
	if err := r.clientStore.Save(&pki.ClientBundle{
		Key: key, Cert: cert, CertPEM: []byte(response.GetCertificatePem()), CAPEM: []byte(response.GetCaCertificatePem()),
	}); err != nil {
		return err
	}
	if pem := response.GetAgentCaCertificatePem(); pem != "" {
		_ = r.clientStore.SaveAgentCA([]byte(pem))
	}
	r.renewBackoff = 0
	r.nextRenew = time.Time{}
	return nil
}

func (r *Runtime) matchIngestCertificate(ctx context.Context, helloUUID, credentialUUID []byte) error {
	ident, hasCert, err := pki.PeerDeviceIdentityFromContext(ctx)
	if err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	if r.forceAgentIngest && !hasCert {
		return status.Error(codes.Unauthenticated, "agent certificate is required")
	}
	if !hasCert {
		return nil
	}
	if ident.Kind != pki.DeviceAgent {
		return status.Error(codes.PermissionDenied, "ingest requires an agent certificate")
	}
	if !subtleBytesEqual(ident.NodeUUID, helloUUID) || !subtleBytesEqual(ident.NodeUUID, credentialUUID) {
		return status.Error(codes.PermissionDenied, "certificate UUID does not match hello or credential")
	}
	return nil
}

type collectorTokenCredential struct{ token string }

func (c *collectorTokenCredential) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"collector_token": c.token}, nil
}

func (c *collectorTokenCredential) RequireTransportSecurity() bool { return false }

type collectorBootstrapCredential struct{ token string }

func (c *collectorBootstrapCredential) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"collector_token": c.token}, nil
}

func (c *collectorBootstrapCredential) RequireTransportSecurity() bool { return true }

func subtleBytesEqual(left, right []byte) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare(left, right) == 1
}
