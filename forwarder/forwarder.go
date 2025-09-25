package forwarder

import (
	"context"
	"fmt"
	"sync"
	"time"

	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	"github.com/ravichandra-eluri/store-forward-otel/buffer"
)

// Config holds forwarder configuration.
type Config struct {
	Endpoint        string
	BufferDir       string
	MaxBufferSizeMB int64
	RetryInterval   time.Duration
	FlushTimeout    time.Duration
}

// Forwarder implements TraceServiceServer — it receives OTLP spans, attempts
// to forward them to the configured backend, and buffers to disk on failure.
type Forwarder struct {
	collectortrace.UnimplementedTraceServiceServer

	cfg  Config
	buf  *buffer.DiskBuffer
	log  *zap.Logger
	mu   sync.Mutex
	conn *grpc.ClientConn
	cli  collectortrace.TraceServiceClient
}

// New constructs a Forwarder and initialises the disk buffer.
func New(cfg Config, log *zap.Logger) (*Forwarder, error) {
	buf, err := buffer.New(cfg.BufferDir, cfg.MaxBufferSizeMB)
	if err != nil {
		return nil, err
	}
	return &Forwarder{cfg: cfg, buf: buf, log: log}, nil
}

// Export implements collectortrace.TraceServiceServer.
// Spans are forwarded immediately or written to disk when the backend is down.
func (f *Forwarder) Export(
	ctx context.Context,
	req *collectortrace.ExportTraceServiceRequest,
) (*collectortrace.ExportTraceServiceResponse, error) {
	if err := f.tryExport(ctx, req); err != nil {
		f.log.Warn("export failed, buffering spans", zap.Error(err))
		if bufErr := f.bufferReq(req); bufErr != nil {
			return nil, fmt.Errorf("export and buffer both failed: %w", bufErr)
		}
	}
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

// Run starts the background retry loop. Blocks until ctx is cancelled.
func (f *Forwarder) Run(ctx context.Context) {
	ticker := time.NewTicker(f.cfg.RetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.flush(ctx)
		}
	}
}

// Close releases the upstream gRPC connection.
func (f *Forwarder) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conn != nil {
		return f.conn.Close()
	}
	return nil
}

// BufferedBytes returns the current disk buffer usage in bytes.
func (f *Forwarder) BufferedBytes() int64 { return f.buf.Size() }

// --- internal ---

func (f *Forwarder) bufferReq(req *collectortrace.ExportTraceServiceRequest) error {
	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal spans: %w", err)
	}
	return f.buf.Write(data)
}

func (f *Forwarder) flush(ctx context.Context) {
	batches, paths, err := f.buf.Drain()
	if err != nil || len(batches) == 0 {
		return
	}

	f.log.Info("flushing buffered spans", zap.Int("batches", len(batches)))

	var delivered []string
	for i, data := range batches {
		req := &collectortrace.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(data, req); err != nil {
			f.log.Error("corrupt buffer entry, discarding", zap.String("path", paths[i]))
			delivered = append(delivered, paths[i])
			continue
		}

		fctx, cancel := context.WithTimeout(ctx, f.cfg.FlushTimeout)
		exportErr := f.tryExport(fctx, req)
		cancel()

		if exportErr != nil {
			f.log.Warn("flush attempt failed, will retry", zap.Error(exportErr))
			break
		}
		delivered = append(delivered, paths[i])
	}

	f.buf.Delete(delivered)
	if len(delivered) > 0 {
		f.log.Info("flushed buffered spans", zap.Int("count", len(delivered)))
	}
}

func (f *Forwarder) tryExport(ctx context.Context, req *collectortrace.ExportTraceServiceRequest) error {
	cli, err := f.client()
	if err != nil {
		return err
	}
	_, err = cli.Export(ctx, req)
	return err
}

func (f *Forwarder) client() (collectortrace.TraceServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cli != nil {
		return f.cli, nil
	}
	//nolint:staticcheck
	conn, err := grpc.Dial(f.cfg.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", f.cfg.Endpoint, err)
	}
	f.conn = conn
	f.cli = collectortrace.NewTraceServiceClient(conn)
	return f.cli, nil
}
