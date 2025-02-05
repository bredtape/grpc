package grpc_conn

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bredtape/retry"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	backoff = retry.Must(retry.NewExp(0.2, 1*time.Second, 5*time.Second))

	DefaultOptions = Options{
		RetryConnect: backoff,
		DialOptions: []grpc.DialOption{
			grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
			grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor)}}

	OptionsInsecure = Options{
		RetryConnect: backoff,
		DialOptions: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
			grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor)}}

	ErrShutdown = errors.New("shutdown in progress")
)

type Options struct {
	RetryConnect retry.Retryer
	DialOptions  []grpc.DialOption
}

type Conn struct {
	name    string
	address string
	options Options

	once     sync.Once
	requests chan *grpc.ClientConn
}

// New named gRPC connection with address and optional (0..1) Options. Will default to 'DefaultOptions' is not specified
// Remember to call Start!
func New(name, address string, opts ...Options) (*Conn, error) {
	if len(name) == 0 {
		return nil, errors.New("specify name")
	}

	if strings.TrimSpace(address) == "" {
		return nil, errors.New("empty address")
	}

	if len(opts) > 1 {
		return nil, errors.New("specify 0..1 Options")
	}

	c := &Conn{
		name:     name,
		address:  address,
		requests: make(chan *grpc.ClientConn)}

	if len(opts) == 0 {
		c.options = DefaultOptions
	} else {
		c.options = opts[0]
	}

	return c, nil
}

// start connecting and answer requests (in separate go-routine)
func (c *Conn) Start(ctx context.Context) {
	c.once.Do(func() { go c.loop(ctx) })
}

func (c *Conn) GetName() string {
	return c.name
}

func (c *Conn) GetAddress() string {
	return c.address
}

func (c *Conn) GetOptions() Options {
	return c.options
}

// try to obtain connection until the context expires. The *Conn must have been Start'ed
func (c *Conn) GetConnection(ctx context.Context) (*grpc.ClientConn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn, ok := <-c.requests:
		if !ok {
			return nil, ErrShutdown
		}
		return conn, nil
	}
}

func (c *Conn) loop(ctx context.Context) {
	log := slog.With(
		"context", "gRPC conn",
		"name", c.name,
		"address", c.address)

	defer log.Debug("shutdown")
	defer close(c.requests)

	labels := c.getMetricLabelValues()

	// init labels
	metric_grpc_conns.WithLabelValues(labels...)
	metric_grpc_conns_err.WithLabelValues(labels...)

	attempt := 0
	for {
		metric_grpc_conns.WithLabelValues(labels...).Inc()
		log.Debug("dialing")

		conn, err := grpc.NewClient(c.address, c.options.DialOptions...)
		if err != nil {
			log.Error("failed to dial, will retry", "err", err)
			metric_grpc_conns_err.WithLabelValues(labels...).Inc()

			select {
			case <-ctx.Done():
				return
			case <-time.After(c.options.RetryConnect.Next(attempt)):
				attempt++
				continue
			}
		}
		defer conn.Close()

		go c.watchConnectionState(ctx, conn)

		// serve requests until context is done
		for {
			select {
			case <-ctx.Done():
				return
			case c.requests <- conn:
			}
		}
	}
}

func (c *Conn) watchConnectionState(ctx context.Context, conn *grpc.ClientConn) {
	log := slog.With(
		"context", "gRPC conn",
		"name", c.name,
		"address", c.address)

	m := metric_conn_state.WithLabelValues(c.getMetricLabelValues()...)
	state := conn.GetState()
	m.Set(float64(state))
	log.Debug("connection state", "state", state)

	// loop until ctx expires
	for conn.WaitForStateChange(ctx, state) {
		state = conn.GetState()
		m.Set(float64(state))
		log.Debug("connection state changed", "state", state)
	}
}

func (c *Conn) getMetricLabelValues() []string {
	return []string{c.name, c.address}
}
