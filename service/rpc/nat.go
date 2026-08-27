package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const natOpenTimeout = 12 * time.Second

type natTunnel struct {
	serverID uint64
	streamID string
	user     io.ReadWriteCloser
	expires  time.Time
	ready    chan error
	done     chan error
	readyMu  sync.Once
	doneMu   sync.Once
}

func (t *natTunnel) signalReady(err error) {
	t.readyMu.Do(func() { t.ready <- err })
}

func (t *natTunnel) finish(err error) {
	t.doneMu.Do(func() {
		_ = t.user.Close()
		t.done <- err
	})
}

var natTunnels = struct {
	sync.RWMutex
	items map[string]*natTunnel
}{items: make(map[string]*natTunnel)}

func parseNATTarget(target string) (string, uint32, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", 0, errors.New("NAT target is empty")
	}
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return "", 0, errors.New("NAT target must use host:port format")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return "", 0, errors.New("NAT target port is invalid")
	}
	if strings.TrimSpace(host) == "" {
		return "", 0, errors.New("NAT target host is empty")
	}
	return host, uint32(port), nil
}

// ValidateNATConnection checks the typed NAT capability before the HTTP
// connection is hijacked by the gateway.
func ValidateNATConnection(serverID uint64, target string) error {
	session := controlSessionForServer(serverID)
	if session == nil {
		return errors.New("agent is not connected to the control stream")
	}
	if !session.supports(pb.AgentCapability_AGENT_CAPABILITY_NAT) {
		return errors.New("agent did not enable the NAT capability")
	}
	_, _, err := parseNATTarget(target)
	return err
}

func newNATStreamID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// ConnectNAT attaches an authenticated HTTP connection to one explicitly
// authorized agent NAT stream. The target is fixed by the Primary-side config.
func ConnectNAT(ctx context.Context, serverID uint64, target string, user io.ReadWriteCloser) error {
	if user == nil {
		return errors.New("NAT client connection is nil")
	}
	if err := ValidateNATConnection(serverID, target); err != nil {
		return err
	}
	host, port, _ := parseNATTarget(target)
	streamID, err := newNATStreamID()
	if err != nil {
		return err
	}
	tunnel := &natTunnel{
		serverID: serverID, streamID: streamID, user: user,
		expires: time.Now().Add(natOpenTimeout), ready: make(chan error, 1), done: make(chan error, 1),
	}
	natTunnels.Lock()
	natTunnels.items[streamID] = tunnel
	natTunnels.Unlock()
	defer func() {
		natTunnels.Lock()
		if natTunnels.items[streamID] == tunnel {
			delete(natTunnels.items, streamID)
		}
		natTunnels.Unlock()
		tunnel.finish(context.Canceled)
	}()

	session := controlSessionForServer(serverID)
	if session == nil || !session.supports(pb.AgentCapability_AGENT_CAPABILITY_NAT) {
		return errors.New("agent NAT capability became unavailable")
	}
	if err := session.send(&pb.PrimaryControlResponse{Body: &pb.PrimaryControlResponse_NatOpenRequest{NatOpenRequest: &pb.NATOpenRequest{
		StreamId: streamID, TargetHost: host, TargetPort: port, ExpiresAtUnix: tunnel.expires.Unix(),
	}}}); err != nil {
		return fmt.Errorf("request agent NAT stream: %w", err)
	}

	openTimer := time.NewTimer(natOpenTimeout)
	defer openTimer.Stop()
	select {
	case err := <-tunnel.ready:
		if err != nil {
			return err
		}
	case <-openTimer.C:
		return errors.New("agent NAT stream timed out")
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-tunnel.done:
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func resolveNATOpenResult(serverID uint64, result *pb.NATOpenResult) {
	if result == nil || result.GetStreamId() == "" || result.GetAccepted() {
		return
	}
	natTunnels.RLock()
	tunnel := natTunnels.items[result.GetStreamId()]
	natTunnels.RUnlock()
	if tunnel == nil || tunnel.serverID != serverID {
		return
	}
	message := strings.TrimSpace(result.GetError())
	if message == "" {
		message = "agent rejected the NAT stream"
	}
	tunnel.signalReady(errors.New(message))
	tunnel.finish(errors.New(message))
}

func (h *V2Handler) NATStream(stream grpc.BidiStreamingServer[pb.NATFrame, pb.NATFrame]) error {
	serverID, err := h.authenticateAgent(stream.Context())
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.GetKind() != pb.NATFrameKind_NAT_FRAME_KIND_OPEN || first.GetStreamId() == "" {
		return status.Error(codes.InvalidArgument, "the first NAT frame must open a stream")
	}
	natTunnels.RLock()
	tunnel := natTunnels.items[first.GetStreamId()]
	natTunnels.RUnlock()
	if tunnel == nil || tunnel.serverID != serverID {
		return status.Error(codes.PermissionDenied, "NAT stream is not authorized")
	}
	if time.Now().After(tunnel.expires) {
		tunnel.signalReady(errors.New("NAT authorization expired"))
		return status.Error(codes.DeadlineExceeded, "NAT stream authorization expired")
	}
	session := controlSessionForServer(serverID)
	if session == nil || !session.supports(pb.AgentCapability_AGENT_CAPABILITY_NAT) {
		tunnel.signalReady(errors.New("agent NAT capability is unavailable"))
		return status.Error(codes.PermissionDenied, "NAT capability is unavailable")
	}
	tunnel.signalReady(nil)
	err = relayPrimaryNAT(stream.Context(), tunnel, stream)
	tunnel.finish(err)
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func relayPrimaryNAT(ctx context.Context, tunnel *natTunnel, stream grpc.BidiStreamingServer[pb.NATFrame, pb.NATFrame]) error {
	readErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			count, err := tunnel.user.Read(buffer)
			if count > 0 {
				payload := append([]byte(nil), buffer[:count]...)
				if sendErr := stream.Send(&pb.NATFrame{StreamId: tunnel.streamID, Kind: pb.NATFrameKind_NAT_FRAME_KIND_DATA, Data: payload}); sendErr != nil {
					readErr <- sendErr
					return
				}
			}
			if err != nil {
				kind := pb.NATFrameKind_NAT_FRAME_KIND_CLOSE
				message := ""
				if !errors.Is(err, io.EOF) {
					kind = pb.NATFrameKind_NAT_FRAME_KIND_ERROR
					message = err.Error()
				}
				_ = stream.Send(&pb.NATFrame{StreamId: tunnel.streamID, Kind: kind, Error: message})
				readErr <- err
				return
			}
		}
	}()
	recv := make(chan *pb.NATFrame, 1)
	recvErr := make(chan error, 1)
	go func() {
		for {
			frame, err := stream.Recv()
			if err != nil {
				select {
				case recvErr <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case recv <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case err := <-recvErr:
			return err
		case frame := <-recv:
			if frame.GetStreamId() != tunnel.streamID {
				return status.Error(codes.PermissionDenied, "NAT stream identifier mismatch")
			}
			switch frame.GetKind() {
			case pb.NATFrameKind_NAT_FRAME_KIND_DATA:
				if _, err := tunnel.user.Write(frame.GetData()); err != nil {
					return err
				}
			case pb.NATFrameKind_NAT_FRAME_KIND_CLOSE:
				return io.EOF
			case pb.NATFrameKind_NAT_FRAME_KIND_ERROR:
				return errors.New(frame.GetError())
			default:
				return status.Error(codes.InvalidArgument, "unexpected NAT frame kind")
			}
		}
	}
}
