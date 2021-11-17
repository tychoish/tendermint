package abciclient_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"math/rand"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/server"
	"github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
)

func TestProperSyncCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := slowApp{}
	logger := log.TestingLogger()

	_, c := setupClientServer(ctx, t, logger, app)

	resp := make(chan error, 1)
	go func() {
		// This is BeginBlockSync unrolled....
		reqres, err := c.BeginBlockAsync(ctx, types.RequestBeginBlock{})
		assert.NoError(t, err)
		err = c.FlushSync(ctx)
		assert.NoError(t, err)
		res := reqres.Response.GetBeginBlock()
		assert.NotNil(t, res)
		resp <- c.Error()
	}()

	select {
	case <-time.After(time.Second):
		require.Fail(t, "No response arrived")
	case err, ok := <-resp:
		require.True(t, ok, "Must not close channel")
		assert.NoError(t, err, "This should return success")
	}
}

func TestHangingSyncCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := slowApp{}
	logger := log.TestingLogger()

	s, c := setupClientServer(ctx, t, logger, app)

	resp := make(chan error, 1)
	go func() {
		// Start BeginBlock and flush it
		reqres, err := c.BeginBlockAsync(ctx, types.RequestBeginBlock{})
		assert.NoError(t, err)
		flush, err := c.FlushAsync(ctx)
		assert.NoError(t, err)
		// wait 20 ms for all events to travel socket, but
		// no response yet from server
		time.Sleep(20 * time.Millisecond)
		// kill the server, so the connections break
		cancel()
		s.Wait()

		// wait for the response from BeginBlock
		reqres.Wait()
		flush.Wait()
		resp <- c.Error()
	}()

	select {
	case <-time.After(time.Second):
		require.Fail(t, "No response arrived")
	case err, ok := <-resp:
		require.True(t, ok, "Must not close channel")
		assert.Error(t, err, "We should get EOF error")
	}
}

func setupClientServer(
	ctx context.Context,
	t *testing.T,
	logger log.Logger,
	app types.Application,
) (service.Service, abciclient.Client) {
	t.Helper()

	// some port between 20k and 30k
	port := 20000 + rand.Int31()%10000
	addr := fmt.Sprintf("localhost:%d", port)

	s, err := server.NewServer(logger, addr, "socket", app)
	require.NoError(t, err)
	err = s.Start(ctx)
	require.NoError(t, err)

	c := abciclient.NewSocketClient(logger, addr, true)
	err = c.Start(ctx)
	require.NoError(t, err)

	return s, c
}

type slowApp struct {
	types.BaseApplication
}

func (slowApp) BeginBlock(req types.RequestBeginBlock) types.ResponseBeginBlock {
	time.Sleep(200 * time.Millisecond)
	return types.ResponseBeginBlock{}
}
