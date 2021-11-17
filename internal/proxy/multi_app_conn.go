package proxy

import (
	"fmt"
	"os"
	"sync"
	"syscall"

	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
)

const (
	connConsensus = "consensus"
	connMempool   = "mempool"
	connQuery     = "query"
	connSnapshot  = "snapshot"
)

// AppConns is the Tendermint's interface to the application that consists of
// multiple connections.
type AppConns interface {
	service.Service

	// Mempool connection
	Mempool() AppConnMempool
	// Consensus connection
	Consensus() AppConnConsensus
	// Query connection
	Query() AppConnQuery
	// Snapshot connection
	Snapshot() AppConnSnapshot
}

// NewAppConns calls NewMultiAppConn.
func NewAppConns(clientCreator abciclient.Creator, logger log.Logger, metrics *Metrics) AppConns {
	return NewMultiAppConn(clientCreator, logger, metrics)
}

// multiAppConn implements AppConns.
//
// A multiAppConn is made of a few appConns and manages their underlying abci
// clients.
// TODO: on app restart, clients must reboot together
type multiAppConn struct {
	service.BaseService

	metrics       *Metrics
	consensusConn AppConnConsensus
	mempoolConn   AppConnMempool
	queryConn     AppConnQuery
	snapshotConn  AppConnSnapshot

	consensusConnClient abciclient.Client
	mempoolConnClient   abciclient.Client
	queryConnClient     abciclient.Client
	snapshotConnClient  abciclient.Client

	clientCreator abciclient.Creator
}

// NewMultiAppConn makes all necessary abci connections to the application.
func NewMultiAppConn(clientCreator abciclient.Creator, logger log.Logger, metrics *Metrics) AppConns {
	multiAppConn := &multiAppConn{
		metrics:       metrics,
		clientCreator: clientCreator,
	}
	multiAppConn.BaseService = *service.NewBaseService(logger, "multiAppConn", multiAppConn)
	return multiAppConn
}

func (app *multiAppConn) Mempool() AppConnMempool {
	return app.mempoolConn
}

func (app *multiAppConn) Consensus() AppConnConsensus {
	return app.consensusConn
}

func (app *multiAppConn) Query() AppConnQuery {
	return app.queryConn
}

func (app *multiAppConn) Snapshot() AppConnSnapshot {
	return app.snapshotConn
}

func (app *multiAppConn) OnStart() error {
	c, err := app.abciClientFor(connQuery)
	if err != nil {
		return err
	}
	app.queryConnClient = c
	app.queryConn = NewAppConnQuery(c, app.metrics)

	c, err = app.abciClientFor(connSnapshot)
	if err != nil {
		app.stopAllClients()
		return err
	}
	app.snapshotConnClient = c
	app.snapshotConn = NewAppConnSnapshot(c, app.metrics)

	c, err = app.abciClientFor(connMempool)
	if err != nil {
		app.stopAllClients()
		return err
	}
	app.mempoolConnClient = c
	app.mempoolConn = NewAppConnMempool(c, app.metrics)

	c, err = app.abciClientFor(connConsensus)
	if err != nil {
		app.stopAllClients()
		return err
	}
	app.consensusConnClient = c
	app.consensusConn = NewAppConnConsensus(c, app.metrics)

	// Kill Tendermint if the ABCI application crashes.
	go app.killTMOnClientError(ctx)

	return nil
}

func (app *multiAppConn) OnStop() {
	app.stopAllClients()
}

func (app *multiAppConn) killTMOnClientError(ctx context.Context) {
	killFn := func(conn string, err error, logger log.Logger) {
		logger.Error(
			fmt.Sprintf("%s connection terminated. Did the application crash? Please restart tendermint", conn),
			"err", err)
		if killErr := kill(); killErr != nil {
			logger.Error("Failed to kill this process - please do so manually", "err", killErr)
		}
	}

	type op struct {
		connClient stoppableClient
		name       string
	}

	wg := &sync.WaitGroup{}
	for _, client := range []op{
		{
			connClient: app.consensusConnClient,
			name:       connConsensus,
		},
		{
			connClient: app.mempoolConnClient,
			name:       connMempool,
		},
		{
			connClient: app.queryConnClient,
			name:       connQuery,
		},
		{
			connClient: app.snapshotConnClient,
			name:       connSnapshot,
		},
	} {
		wg.Add(1)
		go func(client op) {
			defer wg.Done()

			select {
			case <-ctx.Done():
			case <-signalWait(client.connClient.Wait):
				if err := client.connClient.Error(); err != nil {
					killFn(client.name, err, app.Logger)
				}
			}
		}(client)
	}

	// convert wg into a signal channel so we can cleanup if no
	// one is waiting
	select {
	case <-ctx.Done():
	case <-signalWait(wg.Wait):
	}
}

func signalWait(wait func()) chan struct{} {
	out := make(chan struct{})

	go func() {
		defer close(out)
		wait()
	}()

	return out
}

func (app *multiAppConn) stopAllClients() {
	if app.consensusConnClient != nil {
		if err := app.consensusConnClient.Stop(); err != nil {
			app.Logger.Error("error while stopping consensus client", "error", err)
		}
	}
	if app.mempoolConnClient != nil {
		if err := app.mempoolConnClient.Stop(); err != nil {
			app.Logger.Error("error while stopping mempool client", "error", err)
		}
	}
	if app.queryConnClient != nil {
		if err := app.queryConnClient.Stop(); err != nil {
			app.Logger.Error("error while stopping query client", "error", err)
		}
	}
	if app.snapshotConnClient != nil {
		if err := app.snapshotConnClient.Stop(); err != nil {
			app.Logger.Error("error while stopping snapshot client", "error", err)
		}
	}
}

func (app *multiAppConn) abciClientFor(conn string) (abciclient.Client, error) {
	c, err := app.clientCreator(app.Logger.With(
		"module", "abci-client",
		"connection", conn))
	if err != nil {
		return nil, fmt.Errorf("error creating ABCI client (%s connection): %w", conn, err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("error starting ABCI client (%s connection): %w", conn, err)
	}
	return c, nil
}

func kill() error {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}

	return p.Signal(syscall.SIGTERM)
}
