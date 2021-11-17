package consensus

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/eventbus"
	"github.com/tendermint/tendermint/internal/proxy"
	sm "github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/internal/store"
	"github.com/tendermint/tendermint/libs/log"
	tmpubsub "github.com/tendermint/tendermint/libs/pubsub"
	"github.com/tendermint/tendermint/types"
)

const (
	// event bus subscriber
	subscriber = "replay-file"
)

//--------------------------------------------------------
// replay messages interactively or all at once

// replay the wal file
func RunReplayFile(
	ctx context.Context,
	logger log.Logger,
	cfg config.BaseConfig,
	csConfig *config.ConsensusConfig,
	console bool,
) error {
	consensusState, err := newConsensusStateForReplay(ctx, cfg, logger, csConfig)
	if err != nil {
		return err
	}

	if err := consensusState.ReplayFile(csConfig.WalFile(), console); err != nil {
		return fmt.Errorf("consensus replay: %w", err)
	}

	return nil
}

// Replay msgs in file or start the console
func (cs *State) ReplayFile(file string, console bool) error {

	if cs.IsRunning() {
		return errors.New("cs is already running, cannot replay")
	}
	if cs.wal != nil {
		return errors.New("cs wal is open, cannot replay")
	}

	cs.startForReplay()

	// ensure all new step events are regenerated as expected

	ctx := context.Background()
	newStepSub, err := cs.eventBus.SubscribeWithArgs(ctx, tmpubsub.SubscribeArgs{
		ClientID: subscriber,
		Query:    types.EventQueryNewRoundStep,
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe %s to %v", subscriber, types.EventQueryNewRoundStep)
	}
	defer func() {
		args := tmpubsub.UnsubscribeArgs{Subscriber: subscriber, Query: types.EventQueryNewRoundStep}
		if err := cs.eventBus.Unsubscribe(ctx, args); err != nil {
			cs.Logger.Error("Error unsubscribing to event bus", "err", err)
		}
	}()

	// just open the file for reading, no need to use wal
	fp, err := os.OpenFile(file, os.O_RDONLY, 0600)
	if err != nil {
		return err
	}

	pb := newPlayback(file, fp, cs, cs.state.Copy())
	defer pb.fp.Close()

	var nextN int // apply N msgs in a row
	var msg *TimedWALMessage
	for {
		if nextN == 0 && console {
			nextN, err = pb.replayConsoleLoop()
			if err != nil {
				return err
			}
		}

		msg, err = pb.dec.Decode()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		if err := pb.cs.readReplayMessage(msg, newStepSub); err != nil {
			return err
		}

		if nextN > 0 {
			nextN--
		}
		pb.count++
	}
}

//------------------------------------------------
// playback manager

type playback struct {
	cs *State

	fp    *os.File
	dec   *WALDecoder
	count int // how many lines/msgs into the file are we

	// replays can be reset to beginning
	fileName     string   // so we can close/reopen the file
	genesisState sm.State // so the replay session knows where to restart from
}

func newPlayback(fileName string, fp *os.File, cs *State, genState sm.State) *playback {
	return &playback{
		cs:           cs,
		fp:           fp,
		fileName:     fileName,
		genesisState: genState,
		dec:          NewWALDecoder(fp),
	}
}

// go back count steps by resetting the state and running (pb.count - count) steps
func (pb *playback) replayReset(count int, newStepSub eventbus.Subscription) error {
	if err := pb.cs.Stop(); err != nil {
		return err
	}
	pb.cs.Wait()

	newCS := NewState(pb.cs.Logger, pb.cs.config, pb.genesisState.Copy(), pb.cs.blockExec,
		pb.cs.blockStore, pb.cs.txNotifier, pb.cs.evpool)
	newCS.SetEventBus(pb.cs.eventBus)
	newCS.startForReplay()

	if err := pb.fp.Close(); err != nil {
		return err
	}
	fp, err := os.OpenFile(pb.fileName, os.O_RDONLY, 0600)
	if err != nil {
		return err
	}
	pb.fp = fp
	pb.dec = NewWALDecoder(fp)
	count = pb.count - count
	fmt.Printf("Reseting from %d to %d\n", pb.count, count)
	pb.count = 0
	pb.cs = newCS
	var msg *TimedWALMessage
	for i := 0; i < count; i++ {
		msg, err = pb.dec.Decode()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if err := pb.cs.readReplayMessage(msg, newStepSub); err != nil {
			return err
		}
		pb.count++
	}
	return nil
}

func (cs *State) startForReplay() {
	cs.Logger.Error("Replay commands are disabled until someone updates them and writes tests")
	/* TODO:!
	// since we replay tocks we just ignore ticks
		go func() {
			for {
				select {
				case <-cs.tickChan:
				case <-cs.Quit:
					return
				}
			}
		}()*/
}

// console function for parsing input and running commands. The integer
// return value is invalid unless the error is nil.
func (pb *playback) replayConsoleLoop() (int, error) {
	for {
		fmt.Printf("> ")
		bufReader := bufio.NewReader(os.Stdin)
		line, more, err := bufReader.ReadLine()
		if more {
			return 0, fmt.Errorf("input is too long")
		} else if err != nil {
			return 0, err
		}

		tokens := strings.Split(string(line), " ")
		if len(tokens) == 0 {
			continue
		}

		switch tokens[0] {
		case "next":
			// "next" -> replay next message
			// "next N" -> replay next N messages

			if len(tokens) == 1 {
				return 0, nil
			}
			i, err := strconv.Atoi(tokens[1])
			if err != nil {
				fmt.Println("next takes an integer argument")
			} else {
				return i, nil
			}

		case "back":
			// "back" -> go back one message
			// "back N" -> go back N messages

			// NOTE: "back" is not supported in the state machine design,
			// so we restart and replay up to

			ctx := context.TODO()
			// ensure all new step events are regenerated as expected

			newStepSub, err := pb.cs.eventBus.SubscribeWithArgs(ctx, tmpubsub.SubscribeArgs{
				ClientID: subscriber,
				Query:    types.EventQueryNewRoundStep,
			})
			if err != nil {
				return 0, fmt.Errorf("failed to subscribe %s to %v", subscriber, types.EventQueryNewRoundStep)
			}
			defer func() {
				args := tmpubsub.UnsubscribeArgs{Subscriber: subscriber, Query: types.EventQueryNewRoundStep}
				if err := pb.cs.eventBus.Unsubscribe(ctx, args); err != nil {
					pb.cs.Logger.Error("Error unsubscribing from eventBus", "err", err)
				}
			}()

			if len(tokens) == 1 {
				if err := pb.replayReset(1, newStepSub); err != nil {
					pb.cs.Logger.Error("Replay reset error", "err", err)
				}
			} else {
				i, err := strconv.Atoi(tokens[1])
				if err != nil {
					fmt.Println("back takes an integer argument")
				} else if i > pb.count {
					fmt.Printf("argument to back must not be larger than the current count (%d)\n", pb.count)
				} else if err := pb.replayReset(i, newStepSub); err != nil {
					pb.cs.Logger.Error("Replay reset error", "err", err)
				}
			}

		case "rs":
			// "rs" -> print entire round state
			// "rs short" -> print height/round/step
			// "rs <field>" -> print another field of the round state

			rs := pb.cs.RoundState
			if len(tokens) == 1 {
				fmt.Println(rs)
			} else {
				switch tokens[1] {
				case "short":
					fmt.Printf("%v/%v/%v\n", rs.Height, rs.Round, rs.Step)
				case "validators":
					fmt.Println(rs.Validators)
				case "proposal":
					fmt.Println(rs.Proposal)
				case "proposal_block":
					fmt.Printf("%v %v\n", rs.ProposalBlockParts.StringShort(), rs.ProposalBlock.StringShort())
				case "locked_round":
					fmt.Println(rs.LockedRound)
				case "locked_block":
					fmt.Printf("%v %v\n", rs.LockedBlockParts.StringShort(), rs.LockedBlock.StringShort())
				case "votes":
					fmt.Println(rs.Votes.StringIndented("  "))

				default:
					fmt.Println("Unknown option", tokens[1])
				}
			}
		case "n":
			fmt.Println(pb.count)
		}
	}
}

//--------------------------------------------------------------------------------

// convenience for replay mode
func newConsensusStateForReplay(
	ctx context.Context,
	cfg config.BaseConfig,
	logger log.Logger,
	csConfig *config.ConsensusConfig,
) (*State, error) {
	dbType := dbm.BackendType(cfg.DBBackend)
	// Get BlockStore
	blockStoreDB, err := dbm.NewDB("blockstore", dbType, cfg.DBDir())
	if err != nil {
		return nil, err
	}
	blockStore := store.NewBlockStore(blockStoreDB)

	// Get State
	stateDB, err := dbm.NewDB("state", dbType, cfg.DBDir())
	if err != nil {
		return nil, err
	}

	stateStore := sm.NewStore(stateDB)
	gdoc, err := sm.MakeGenesisDocFromFile(cfg.GenesisFile())
	if err != nil {
		return nil, err
	}

	state, err := sm.MakeGenesisState(gdoc)
	if err != nil {
		return nil, err
	}

	// Create proxyAppConn connection (consensus, mempool, query)
	clientCreator, _ := proxy.DefaultClientCreator(logger, cfg.ProxyApp, cfg.ABCI, cfg.DBDir())
	proxyApp := proxy.NewAppConns(clientCreator, logger, proxy.NopMetrics())
	err = proxyApp.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting proxy app conns: %w", err)
	}

	eventBus := eventbus.NewDefault(logger)
	if err := eventBus.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start event bus: %w", err)
	}

	handshaker := NewHandshaker(logger, stateStore, state, blockStore, eventBus, gdoc)

	if err = handshaker.Handshake(ctx, proxyApp); err != nil {
		return nil, err
	}

	mempool, evpool := emptyMempool{}, sm.EmptyEvidencePool{}
	blockExec := sm.NewBlockExecutor(stateStore, logger, proxyApp.Consensus(), mempool, evpool, blockStore)

	consensusState := NewState(logger, csConfig, state.Copy(), blockExec,
		blockStore, mempool, evpool)

	consensusState.SetEventBus(eventBus)
	return consensusState, nil
}
