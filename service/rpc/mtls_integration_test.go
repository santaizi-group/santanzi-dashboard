package rpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/pki"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"github.com/hi2shark/santaizi-dashboard/service/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type primaryFixture struct {
	bundle    *pki.Bundle
	serverPEM []byte
	lis       *bufconn.Listener
	server    *grpc.Server
	secret    string
	serverID  uint64
	node      []byte
	collector *model.Collector
	token     string
}

func setupPrimaryGRPC(t *testing.T, requireMTLS bool) *primaryFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{},
		&model.Collector{}, &model.CollectorScope{}, &model.ServerRuntime{},
		&model.TelemetryEvent{}, &model.TelemetryObservation{}, &model.TelemetryGap{},
		&model.TelemetryIngestCursor{}, &model.AgentTelemetryRuntime{},
		&model.CollectorReplicationReceipt{}, &model.CollectorRuntime{},
	); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	bundle, err := pki.LoadOrCreate(filepath.Join(tmp, "pki"))
	if err != nil {
		t.Fatal(err)
	}
	_, serverPEM, serverKey, err := pki.NewSelfSignedServerCertificate([]string{"localhost"}, time.Now(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certFile := filepath.Join(tmp, "server.crt")
	keyFile := filepath.Join(tmp, "server.key")
	if err := os.WriteFile(certFile, serverPEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, serverKey, 0600); err != nil {
		t.Fatal(err)
	}
	previousDB, previousConf, previousList, previousSecrets := singleton.DB, singleton.Conf, singleton.ServerList, singleton.SecretToID
	singleton.DB = db
	singleton.InitServer()
	secret := "test-client-secret"
	serverID := uint64(7)
	singleton.ServerList[serverID] = &model.Server{Common: model.Common{ID: serverID}, Name: "node-7", Secret: secret}
	singleton.SecretToID[secret] = serverID
	if err := db.Create(&model.Server{Common: model.Common{ID: serverID}, Name: "node-7", Secret: secret}).Error; err != nil {
		t.Fatal(err)
	}
	plain, hash, err := telemetry.NewRegistrationToken()
	if err != nil {
		t.Fatal(err)
	}
	collector := model.Collector{
		CollectorUUID: "collector-edge", Name: "Edge", Address: "edge:5556",
		TokenHash: hash, RegistrationToken: plain, Generation: 1, ConfigVersion: 1,
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	singleton.Conf = &model.Config{
		GRPCPort: 5555,
		GRPCTLS: model.GRPCTLSConfig{
			Enabled: true, CertFile: certFile, KeyFile: keyFile,
			RequireAgentMTLS: requireMTLS, RequireCollectorMTLS: requireMTLS,
		},
		Telemetry: model.TelemetryConfig{
			SigningKeyPath: filepath.Join(tmp, "signing.key"), IngestQueueSize: 32, IngestBatchSize: 64,
			CredentialValidityDays: 30, AvailabilityBucketSeconds: 30, PrimaryEndpoint: "localhost:5555",
		},
	}
	t.Cleanup(func() {
		singleton.DB = previousDB
		singleton.Conf = previousConf
		singleton.ServerList = previousList
		singleton.SecretToID = previousSecrets
		_ = singleton.CloseDB(db)
	})

	creds, err := pki.GRPCServerTLS(pki.ServerTLSOptions{
		CertFile: certFile, KeyFile: keyFile, AgentCA: bundle.Agent.Cert, CollectorCA: bundle.Collector.Cert,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := DeviceAuthPolicy{RequireAgentMTLS: requireMTLS, RequireCollectorMTLS: requireMTLS}
	server := grpc.NewServer(
		grpc.Creds(creds),
		grpc.ChainUnaryInterceptor(UnaryDeviceAuth(policy)),
		grpc.ChainStreamInterceptor(StreamDeviceAuth(policy)),
	)
	pb.RegisterSantaiziEnrollmentServiceServer(server, NewEnrollmentHandler(bundle.Agent))
	v2, err := NewV2Handler()
	if err != nil {
		t.Fatal(err)
	}
	pb.RegisterSantaiziControlServiceServer(server, v2)
	pb.RegisterSantaiziTelemetryServiceServer(server, v2)
	collectorHandler, err := NewPrimaryCollectorHandler(bundle)
	if err != nil {
		t.Fatal(err)
	}
	pb.RegisterSantaiziCollectorServiceServer(server, collectorHandler)
	pb.RegisterSantaiziReplicationServiceServer(server, collectorHandler)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(func() {
		server.Stop()
		_ = lis.Close()
	})
	return &primaryFixture{
		bundle: bundle, serverPEM: serverPEM, lis: lis, server: server,
		secret: secret, serverID: serverID, node: bytes.Repeat([]byte{0x21}, 16),
		collector: &collector, token: plain,
	}
}

func (f *primaryFixture) dial(t *testing.T, clientCert *tls.Certificate, extraRoot []byte, perRPC credentials.PerRPCCredentials) *grpc.ClientConn {
	t.Helper()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(f.serverPEM) {
		t.Fatal("server pem")
	}
	if len(extraRoot) > 0 {
		_ = roots.AppendCertsFromPEM(extraRoot)
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "localhost"}
	if clientCert != nil {
		tlsCfg.Certificates = []tls.Certificate{*clientCert}
		tlsCfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return clientCert, nil
		}
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return f.lis.DialContext(ctx)
		}),
	}
	if perRPC != nil {
		opts = append(opts, grpc.WithPerRPCCredentials(perRPC))
	}
	conn, err := grpc.NewClient("localhost", opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func firstStreamError(header func() (metadata.MD, error), recv func() error) error {
	if _, err := header(); err != nil {
		return err
	}
	return recv()
}

func TestEnrollmentWrongSecret(t *testing.T) {
	f := setupPrimaryGRPC(t, false)
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeAgentURI(f.node))
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, nil, nil, &EnrollmentCredential{ClientSecret: "wrong"})
	_, err = pb.NewSantaiziEnrollmentServiceClient(conn).Enroll(context.Background(), &pb.AgentEnrollRequest{
		NodeUuid: f.node, CsrDer: csr,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong secret: %v", err)
	}
}

func (f *primaryFixture) enrollNode(t *testing.T, node []byte) *tls.Certificate {
	t.Helper()
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeAgentURI(node))
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, nil, nil, &EnrollmentCredential{ClientSecret: f.secret})
	enroll, err := pb.NewSantaiziEnrollmentServiceClient(conn).Enroll(context.Background(), &pb.AgentEnrollRequest{
		NodeUuid: node, CsrDer: csr,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := pki.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair([]byte(enroll.GetCertificatePem()), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return &cert
}

func (f *primaryFixture) holdControl(t *testing.T, node []byte, cert *tls.Certificate) {
	t.Helper()
	conn := f.dial(t, cert, nil, nil)
	control, err := pb.NewSantaiziControlServiceClient(conn).Control(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Send(&pb.AgentControlRequest{Body: &pb.AgentControlRequest_Hello{Hello: &pb.AgentControlHello{
		NodeUuid: node, SessionId: bytes.Repeat([]byte{0x31}, 16), AgentVersion: "test",
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Recv(); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Recv(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentConflictWhenOnline(t *testing.T) {
	f := setupPrimaryGRPC(t, false)
	cert := f.enrollNode(t, f.node)
	f.holdControl(t, f.node, cert)
	other := bytes.Repeat([]byte{0x22}, 16)
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeAgentURI(other))
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, nil, nil, &EnrollmentCredential{ClientSecret: f.secret})
	_, err = pb.NewSantaiziEnrollmentServiceClient(conn).Enroll(context.Background(), &pb.AgentEnrollRequest{
		NodeUuid: other, CsrDer: csr,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("online conflict: %v", err)
	}
}

func TestEnrollmentRebindWhenOffline(t *testing.T) {
	f := setupPrimaryGRPC(t, false)
	_ = f.enrollNode(t, f.node)
	other := bytes.Repeat([]byte{0x22}, 16)
	_ = f.enrollNode(t, other)
	got, err := singleton.ServerIDFromNodeUUID(other)
	if err != nil || got != f.serverID {
		t.Fatalf("rebind serverID=%d err=%v", got, err)
	}
}

func TestFreshEnrollControlIngest(t *testing.T) {
	f := setupPrimaryGRPC(t, true)
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeAgentURI(f.node))
	if err != nil {
		t.Fatal(err)
	}
	enrollConn := f.dial(t, nil, nil, &EnrollmentCredential{ClientSecret: f.secret})
	enroll, err := pb.NewSantaiziEnrollmentServiceClient(enrollConn).Enroll(context.Background(), &pb.AgentEnrollRequest{
		NodeUuid: f.node, CsrDer: csr, AgentVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := pki.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair([]byte(enroll.GetCertificatePem()), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, &clientCert, nil, nil)
	control, err := pb.NewSantaiziControlServiceClient(conn).Control(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := bytes.Repeat([]byte{0x31}, 16)
	if err := control.Send(&pb.AgentControlRequest{Body: &pb.AgentControlRequest_Hello{Hello: &pb.AgentControlHello{
		NodeUuid: f.node, SessionId: session, AgentVersion: "test",
	}}}); err != nil {
		t.Fatal(err)
	}
	credMsg, err := control.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if credMsg.GetCredential() == nil {
		t.Fatal("expected credential")
	}
	assign, err := control.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if assign.GetAssignment() == nil || !assign.GetAssignment().GetEndpoints()[0].GetTls() {
		t.Fatalf("assignment=%v", assign.GetAssignment())
	}

	ingest, err := pb.NewSantaiziTelemetryServiceClient(conn).Ingest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := ingest.Send(&pb.TelemetryRequest{Body: &pb.TelemetryRequest_Hello{Hello: &pb.TelemetryHello{
		NodeUuid: f.node, EndpointId: "primary", Credential: credMsg.GetCredential(), ProtocolVersion: "2",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := ingest.Send(&pb.TelemetryRequest{Body: &pb.TelemetryRequest_Ping{Ping: &pb.TelemetryPing{}}}); err != nil {
		t.Fatal(err)
	}
	pong, err := ingest.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if pong.GetPong() == nil {
		t.Fatal("expected pong")
	}
}

func TestIngestSendsHeadersAfterHello(t *testing.T) {
	f := setupPrimaryGRPC(t, true)
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeAgentURI(f.node))
	if err != nil {
		t.Fatal(err)
	}
	enrollConn := f.dial(t, nil, nil, &EnrollmentCredential{ClientSecret: f.secret})
	enroll, err := pb.NewSantaiziEnrollmentServiceClient(enrollConn).Enroll(context.Background(), &pb.AgentEnrollRequest{
		NodeUuid: f.node, CsrDer: csr, AgentVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := pki.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair([]byte(enroll.GetCertificatePem()), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, &clientCert, nil, nil)
	control, err := pb.NewSantaiziControlServiceClient(conn).Control(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := bytes.Repeat([]byte{0x31}, 16)
	if err := control.Send(&pb.AgentControlRequest{Body: &pb.AgentControlRequest_Hello{Hello: &pb.AgentControlHello{
		NodeUuid: f.node, SessionId: session, AgentVersion: "1.0.0-rs",
	}}}); err != nil {
		t.Fatal(err)
	}
	credMsg, err := control.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if credMsg.GetCredential() == nil {
		t.Fatal("expected credential")
	}
	if _, err := control.Recv(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ingest, err := pb.NewSantaiziTelemetryServiceClient(conn).Ingest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ingest.Send(&pb.TelemetryRequest{Body: &pb.TelemetryRequest_Hello{Hello: &pb.TelemetryHello{
		NodeUuid: f.node, EndpointId: "primary", Credential: credMsg.GetCredential(), ProtocolVersion: "2",
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Header(); err != nil {
		t.Fatalf("ingest headers after hello: %v", err)
	}
}

func TestExistingCertificateSkipsEnrollAndRenews(t *testing.T) {
	f := setupPrimaryGRPC(t, true)
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeAgentURI(f.node))
	if err != nil {
		t.Fatal(err)
	}
	enrollConn := f.dial(t, nil, nil, &EnrollmentCredential{ClientSecret: f.secret})
	enroll, err := pb.NewSantaiziEnrollmentServiceClient(enrollConn).Enroll(context.Background(), &pb.AgentEnrollRequest{
		NodeUuid: f.node, CsrDer: csr,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := pki.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair([]byte(enroll.GetCertificatePem()), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, &clientCert, nil, nil)
	newKey, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	newCSR, err := pki.CreateCSR(newKey, pki.EncodeAgentURI(f.node))
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := pb.NewSantaiziEnrollmentServiceClient(conn).Renew(context.Background(), &pb.AgentRenewRequest{
		NodeUuid: f.node, CsrDer: newCSR,
	})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.GetCertificatePem() == "" || renewed.GetCertificatePem() == enroll.GetCertificatePem() {
		t.Fatal("expected a new certificate")
	}
}

func TestStrictIngestWithoutCert(t *testing.T) {
	f := setupPrimaryGRPC(t, true)
	conn := f.dial(t, nil, nil, nil)
	stream, err := pb.NewSantaiziTelemetryServiceClient(conn).Ingest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&pb.TelemetryRequest{Body: &pb.TelemetryRequest_Ping{Ping: &pb.TelemetryPing{}}})
	err = firstStreamError(stream.Header, func() error {
		_, recvErr := stream.Recv()
		return recvErr
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("strict ingest: %v", err)
	}
}

func TestCollectorRegisterCSRThenMTLSReplicate(t *testing.T) {
	f := setupPrimaryGRPC(t, true)
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeCollectorURI("pending"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := f.dial(t, nil, nil, &CollectorBootstrapCredential{Token: f.token})
	reg, err := pb.NewSantaiziCollectorServiceClient(bootstrap).Register(context.Background(), &pb.RegisterCollectorRequest{
		RegistrationToken: f.token, ProtocolVersion: "2", CsrDer: csr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reg.GetCollectorCertificatePem() == "" || reg.GetCollectorCaCertificatePem() == "" || reg.GetAgentCaCertificatePem() == "" {
		t.Fatalf("missing collector certs: %#v", reg)
	}
	keyPEM, err := pki.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair([]byte(reg.GetCollectorCertificatePem()), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, &clientCert, nil, nil)
	stream, err := pb.NewSantaiziReplicationServiceClient(conn).Replicate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseSend(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil && err != io.EOF {
		t.Fatal(err)
	}

	agentKey, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	agentCSR, err := pki.CreateCSR(agentKey, pki.EncodeAgentURI(f.node))
	if err != nil {
		t.Fatal(err)
	}
	agentConn := f.dial(t, nil, nil, &EnrollmentCredential{ClientSecret: f.secret})
	agentCertPEM, err := pb.NewSantaiziEnrollmentServiceClient(agentConn).Enroll(context.Background(), &pb.AgentEnrollRequest{
		NodeUuid: f.node, CsrDer: agentCSR,
	})
	if err != nil {
		t.Fatal(err)
	}
	agentKeyPEM, err := pki.MarshalPrivateKeyPEM(agentKey)
	if err != nil {
		t.Fatal(err)
	}
	agentTLS, err := tls.X509KeyPair([]byte(agentCertPEM.GetCertificatePem()), agentKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	wrong := f.dial(t, &agentTLS, nil, nil)
	syncStream, err := pb.NewSantaiziCollectorServiceClient(wrong).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = syncStream.Send(&pb.CollectorSyncRequest{Body: &pb.CollectorSyncRequest_Hello{Hello: &pb.CollectorSyncHello{
		CollectorUuid: "collector-edge",
	}}})
	err = firstStreamError(syncStream.Header, func() error {
		_, recvErr := syncStream.Recv()
		return recvErr
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("agent cert on sync: %v", err)
	}
}

func TestCollectorWrongToken(t *testing.T) {
	f := setupPrimaryGRPC(t, false)
	conn := f.dial(t, nil, nil, nil)
	_, err := pb.NewSantaiziCollectorServiceClient(conn).Register(context.Background(), &pb.RegisterCollectorRequest{
		RegistrationToken: "stc_deadbeef", ProtocolVersion: "2",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong token: %v", err)
	}
}

func enrollAgentTLS(t *testing.T, f *primaryFixture) tls.Certificate {
	t.Helper()
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeAgentURI(f.node))
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, nil, nil, &EnrollmentCredential{ClientSecret: f.secret})
	enroll, err := pb.NewSantaiziEnrollmentServiceClient(conn).Enroll(context.Background(), &pb.AgentEnrollRequest{
		NodeUuid: f.node, CsrDer: csr,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := pki.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair([]byte(enroll.GetCertificatePem()), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestIngestHelloUUIDMismatch(t *testing.T) {
	f := setupPrimaryGRPC(t, true)
	cert := enrollAgentTLS(t, f)
	conn := f.dial(t, &cert, nil, nil)
	control, err := pb.NewSantaiziControlServiceClient(conn).Control(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Send(&pb.AgentControlRequest{Body: &pb.AgentControlRequest_Hello{Hello: &pb.AgentControlHello{
		NodeUuid: f.node, SessionId: bytes.Repeat([]byte{0x41}, 16),
	}}}); err != nil {
		t.Fatal(err)
	}
	credMsg, err := control.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if credMsg.GetCredential() == nil {
		t.Fatal("expected credential")
	}
	if _, err := control.Recv(); err != nil {
		t.Fatal(err)
	}
	ingest, err := pb.NewSantaiziTelemetryServiceClient(conn).Ingest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := ingest.Send(&pb.TelemetryRequest{Body: &pb.TelemetryRequest_Hello{Hello: &pb.TelemetryHello{
		NodeUuid: bytes.Repeat([]byte{0x99}, 16), EndpointId: "primary", Credential: credMsg.GetCredential(), ProtocolVersion: "2",
	}}}); err != nil {
		t.Fatal(err)
	}
	_, err = ingest.Recv()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ingest uuid mismatch: %v", err)
	}
}

func TestWrongCAClientCertRejected(t *testing.T) {
	f := setupPrimaryGRPC(t, true)
	other, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeAgentURI(f.node))
	if err != nil {
		t.Fatal(err)
	}
	certPEM, _, _, err := pki.SignAgentCSR(other.Agent, csr, f.node, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := pki.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, &clientCert, nil, nil)
	stream, err := pb.NewSantaiziControlServiceClient(conn).Control(context.Background())
	if err != nil {
		return
	}
	_ = stream.Send(&pb.AgentControlRequest{Body: &pb.AgentControlRequest_Hello{Hello: &pb.AgentControlHello{
		NodeUuid: f.node, SessionId: bytes.Repeat([]byte{0x42}, 16),
	}}})
	err = firstStreamError(stream.Header, func() error {
		_, recvErr := stream.Recv()
		return recvErr
	})
	if err == nil {
		t.Fatal("wrong CA client certificate was accepted")
	}
}

func TestCollectorHelloUUIDMismatch(t *testing.T) {
	f := setupPrimaryGRPC(t, true)
	key, err := pki.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.CreateCSR(key, pki.EncodeCollectorURI("pending"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := f.dial(t, nil, nil, &CollectorBootstrapCredential{Token: f.token})
	reg, err := pb.NewSantaiziCollectorServiceClient(bootstrap).Register(context.Background(), &pb.RegisterCollectorRequest{
		RegistrationToken: f.token, ProtocolVersion: "2", CsrDer: csr,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := pki.MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair([]byte(reg.GetCollectorCertificatePem()), keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, &clientCert, nil, nil)
	stream, err := pb.NewSantaiziCollectorServiceClient(conn).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pb.CollectorSyncRequest{Body: &pb.CollectorSyncRequest_Hello{Hello: &pb.CollectorSyncHello{
		CollectorUuid: "other-collector",
	}}}); err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("hello uuid mismatch: %v", err)
	}
}
