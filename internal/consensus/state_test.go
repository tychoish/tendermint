package consensus

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/abci/example/kvstore"
	"github.com/tendermint/tendermint/crypto/tmhash"
	cstypes "github.com/tendermint/tendermint/internal/consensus/types"
	"github.com/tendermint/tendermint/internal/eventbus"
	tmpubsub "github.com/tendermint/tendermint/internal/pubsub"
	"github.com/tendermint/tendermint/libs/log"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

/*

ProposeSuite
x * TestProposerSelection0 - round robin ordering, round 0
x * TestProposerSelection2 - round robin ordering, round 2++
x * TestEnterProposeNoValidator - timeout into prevote round
x * TestEnterPropose - finish propose without timing out (we have the proposal)
x * TestBadProposal - 2 vals, bad proposal (bad block state hash), should prevote and precommit nil
x * TestOversizedBlock - block with too many txs should be rejected
FullRoundSuite
x * TestFullRound1 - 1 val, full successful round
x * TestFullRoundNil - 1 val, full round of nil
x * TestFullRound2 - 2 vals, both required for full round
LockSuite
x * TestLockNoPOL - 2 vals, 4 rounds. one val locked, precommits nil every round except first.
x * TestLockPOLRelock - 4 vals, one precommits, other 3 polka at next round, so we unlock and precomit the polka
x * TestLockPOLUnlock - 4 vals, one precommits, other 3 polka nil at next round, so we unlock and precomit nil
x * TestLockPOLSafety1 - 4 vals. We shouldn't change lock based on polka at earlier round
x * TestLockPOLSafety2 - 4 vals. After unlocking, we shouldn't relock based on polka at earlier round
  * TestNetworkLock - once +1/3 precommits, network should be locked
  * TestNetworkLockPOL - once +1/3 precommits, the block with more recent polka is committed
SlashingSuite
x * TestSlashingPrevotes - a validator prevoting twice in a round gets slashed
x * TestSlashingPrecommits - a validator precomitting twice in a round gets slashed
CatchupSuite
  * TestCatchup - if we might be behind and we've seen any 2/3 prevotes, round skip to new round, precommit, or prevote
HaltSuite
x * TestHalt1 - if we see +2/3 precommits after timing out into new round, we should still commit

*/

//----------------------------------------------------------------------------------------------------
// ProposeSuite

func TestStateProposerSelection0(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := configSetup(t)

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)

	height, round := cs1.Height, cs1.Round

	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)

	startTestRound(ctx, cs1, height, round)

	// Wait for new round so proposer is set.
	ensureNewRound(t, newRoundCh, height, round)

	// Commit a block and ensure proposer for the next height is correct.
	prop := cs1.GetRoundState().Validators.GetProposer()
	pv, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	address := pv.Address()
	if !bytes.Equal(prop.Address, address) {
		t.Fatalf("expected proposer to be validator %d. Got %X", 0, prop.Address)
	}

	// Wait for complete proposal.
	ensureNewProposal(t, proposalCh, height, round)

	rs := cs1.GetRoundState()
	signAddVotes(
		ctx,
		t,
		config,
		cs1,
		tmproto.PrecommitType,
		rs.ProposalBlock.Hash(),
		rs.ProposalBlockParts.Header(),
		vss[1:]...,
	)

	// Wait for new round so next validator is set.
	ensureNewRound(t, newRoundCh, height+1, 0)

	prop = cs1.GetRoundState().Validators.GetProposer()
	pv1, err := vss[1].GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	require.True(t, bytes.Equal(prop.Address, addr), "expected proposer to be validator %d. Got %X", 1, prop.Address)
}

// Now let's do it all again, but starting from round 2 instead of 0
func TestStateProposerSelection2(t *testing.T) {
	config := configSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4) // test needs more work for more than 3 validators

	height := cs1.Height
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)

	// this time we jump in at round 2
	incrementRound(vss[1:]...)
	incrementRound(vss[1:]...)

	var round int32 = 2
	startTestRound(ctx, cs1, height, round)

	ensureNewRound(t, newRoundCh, height, round) // wait for the new round

	// everyone just votes nil. we get a new proposer each round
	for i := int32(0); int(i) < len(vss); i++ {
		prop := cs1.GetRoundState().Validators.GetProposer()
		pvk, err := vss[int(i+round)%len(vss)].GetPubKey(ctx)
		require.NoError(t, err)
		addr := pvk.Address()
		correctProposer := addr
		require.True(t, bytes.Equal(prop.Address, correctProposer),
			"expected RoundState.Validators.GetProposer() to be validator %d. Got %X",
			int(i+2)%len(vss),
			prop.Address)

		rs := cs1.GetRoundState()
		signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, rs.ProposalBlockParts.Header(), vss[1:]...)
		ensureNewRound(t, newRoundCh, height, i+round+1) // wait for the new round event each round
		incrementRound(vss[1:]...)
	}

}

// a non-validator should timeout into the prevote round
func TestStateEnterProposeNoPrivValidator(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs, _ := randState(ctx, t, config, log.TestingLogger(), 1)

	cs.SetPrivValidator(ctx, nil)
	height, round := cs.Height, cs.Round

	// Listen for propose timeout event
	timeoutCh := subscribe(ctx, t, cs.eventBus, types.EventQueryTimeoutPropose)

	startTestRound(ctx, cs, height, round)

	// if we're not a validator, EnterPropose should timeout
	ensureNewTimeout(t, timeoutCh, height, round, cs.config.TimeoutPropose.Nanoseconds())

	if cs.GetRoundState().Proposal != nil {
		t.Error("Expected to make no proposal, since no privValidator")
	}
}

// a validator should not timeout of the prevote round (TODO: unless the block is really big!)
func TestStateEnterProposeYesPrivValidator(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs, _ := randState(ctx, t, config, log.TestingLogger(), 1)
	height, round := cs.Height, cs.Round

	// Listen for propose timeout event

	timeoutCh := subscribe(ctx, t, cs.eventBus, types.EventQueryTimeoutPropose)
	proposalCh := subscribe(ctx, t, cs.eventBus, types.EventQueryCompleteProposal)

	cs.enterNewRound(ctx, height, round)
	cs.startRoutines(ctx, 3)

	ensureNewProposal(t, proposalCh, height, round)

	// Check that Proposal, ProposalBlock, ProposalBlockParts are set.
	rs := cs.GetRoundState()
	if rs.Proposal == nil {
		t.Error("rs.Proposal should be set")
	}
	if rs.ProposalBlock == nil {
		t.Error("rs.ProposalBlock should be set")
	}
	if rs.ProposalBlockParts.Total() == 0 {
		t.Error("rs.ProposalBlockParts should be set")
	}

	// if we're a validator, enterPropose should not timeout
	ensureNoNewTimeout(t, timeoutCh, cs.config.TimeoutPropose.Nanoseconds())
}

func TestStateBadProposal(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 2)
	height, round := cs1.Height, cs1.Round
	vs2 := vss[1]

	partSize := types.BlockPartSizeBytes

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	voteCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryVote)

	propBlock, _, err := cs1.createProposalBlock() // changeProposer(t, cs1, vs2)
	require.NoError(t, err)

	// make the second validator the proposer by incrementing round
	round++
	incrementRound(vss[1:]...)

	// make the block bad by tampering with statehash
	stateHash := propBlock.AppHash
	if len(stateHash) == 0 {
		stateHash = make([]byte, 32)
	}
	stateHash[0] = (stateHash[0] + 1) % 255
	propBlock.AppHash = stateHash
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: propBlock.Hash(), PartSetHeader: propBlockParts.Header()}
	proposal := types.NewProposal(vs2.Height, round, -1, blockID)
	p := proposal.ToProto()
	if err := vs2.SignProposal(ctx, config.ChainID(), p); err != nil {
		t.Fatal("failed to sign bad proposal", err)
	}

	proposal.Signature = p.Signature

	// set the proposal block
	if err := cs1.SetProposalAndBlock(ctx, proposal, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}

	// start the machine
	startTestRound(ctx, cs1, height, round)

	// wait for proposal
	ensureProposal(t, proposalCh, height, round, blockID)

	// wait for prevote
	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], nil)

	// add bad prevote from vs2 and wait for it
	bps, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlock.Hash(), bps.Header(), vs2)
	ensurePrevote(t, voteCh, height, round)

	// wait for precommit
	ensurePrecommit(t, voteCh, height, round)
	validatePrecommit(ctx, t, cs1, round, -1, vss[0], nil, nil)

	bps2, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, propBlock.Hash(), bps2.Header(), vs2)
}

func TestStateOversizedBlock(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 2)
	cs1.state.ConsensusParams.Block.MaxBytes = 2000
	height, round := cs1.Height, cs1.Round
	vs2 := vss[1]

	partSize := types.BlockPartSizeBytes

	timeoutProposeCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)
	voteCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryVote)

	propBlock, _, err := cs1.createProposalBlock()
	require.NoError(t, err)
	propBlock.Data.Txs = []types.Tx{tmrand.Bytes(2001)}
	propBlock.Header.DataHash = propBlock.Data.Hash()

	// make the second validator the proposer by incrementing round
	round++
	incrementRound(vss[1:]...)

	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: propBlock.Hash(), PartSetHeader: propBlockParts.Header()}
	proposal := types.NewProposal(height, round, -1, blockID)
	p := proposal.ToProto()
	if err := vs2.SignProposal(ctx, config.ChainID(), p); err != nil {
		t.Fatal("failed to sign bad proposal", err)
	}
	proposal.Signature = p.Signature

	totalBytes := 0
	for i := 0; i < int(propBlockParts.Total()); i++ {
		part := propBlockParts.GetPart(i)
		totalBytes += len(part.Bytes)
	}

	if err := cs1.SetProposalAndBlock(ctx, proposal, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}

	// start the machine
	startTestRound(ctx, cs1, height, round)

	t.Log("Block Sizes", "Limit", cs1.state.ConsensusParams.Block.MaxBytes, "Current", totalBytes)

	// c1 should log an error with the block part message as it exceeds the consensus params. The
	// block is not added to cs.ProposalBlock so the node timeouts.
	ensureNewTimeout(t, timeoutProposeCh, height, round, cs1.config.Propose(round).Nanoseconds())

	// and then should send nil prevote and precommit regardless of whether other validators prevote and
	// precommit on it
	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], nil)

	bps, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlock.Hash(), bps.Header(), vs2)
	ensurePrevote(t, voteCh, height, round)
	ensurePrecommit(t, voteCh, height, round)
	validatePrecommit(ctx, t, cs1, round, -1, vss[0], nil, nil)

	bps2, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, propBlock.Hash(), bps2.Header(), vs2)
}

//----------------------------------------------------------------------------------------------------
// FullRoundSuite

// propose, prevote, and precommit a block
func TestStateFullRound1(t *testing.T) {
	config := configSetup(t)
	logger := log.TestingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs, vss := randState(ctx, t, config, logger, 1)
	height, round := cs.Height, cs.Round

	// NOTE: buffer capacity of 0 ensures we can validate prevote and last commit
	// before consensus can move to the next height (and cause a race condition)
	if err := cs.eventBus.Stop(); err != nil {
		t.Error(err)
	}
	eventBus := eventbus.NewDefault(logger.With("module", "events"))

	cs.SetEventBus(eventBus)
	if err := eventBus.Start(ctx); err != nil {
		t.Error(err)
	}

	voteCh := subscribe(ctx, t, cs.eventBus, types.EventQueryVote)
	propCh := subscribe(ctx, t, cs.eventBus, types.EventQueryCompleteProposal)
	newRoundCh := subscribe(ctx, t, cs.eventBus, types.EventQueryNewRound)

	// Maybe it would be better to call explicitly startRoutines(4)
	startTestRound(ctx, cs, height, round)

	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, propCh, height, round)
	propBlockHash := cs.GetRoundState().ProposalBlock.Hash()

	ensurePrevote(t, voteCh, height, round) // wait for prevote
	validatePrevote(ctx, t, cs, round, vss[0], propBlockHash)

	ensurePrecommit(t, voteCh, height, round) // wait for precommit

	// we're going to roll right into new height
	ensureNewRound(t, newRoundCh, height+1, 0)

	validateLastPrecommit(ctx, t, cs, vss[0], propBlockHash)
}

// nil is proposed, so prevote and precommit nil
func TestStateFullRoundNil(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs, vss := randState(ctx, t, config, log.TestingLogger(), 1)
	height, round := cs.Height, cs.Round

	voteCh := subscribe(ctx, t, cs.eventBus, types.EventQueryVote)

	cs.enterPrevote(ctx, height, round)
	cs.startRoutines(ctx, 4)

	ensurePrevote(t, voteCh, height, round)   // prevote
	ensurePrecommit(t, voteCh, height, round) // precommit

	// should prevote and precommit nil
	validatePrevoteAndPrecommit(ctx, t, cs, round, -1, vss[0], nil, nil)
}

// run through propose, prevote, precommit commit with two validators
// where the first validator has to wait for votes from the second
func TestStateFullRound2(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 2)
	vs2 := vss[1]
	height, round := cs1.Height, cs1.Round

	voteCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryVote)
	newBlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewBlock)

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, height, round)

	ensurePrevote(t, voteCh, height, round) // prevote

	// we should be stuck in limbo waiting for more prevotes
	rs := cs1.GetRoundState()
	propBlockHash, propPartSetHeader := rs.ProposalBlock.Hash(), rs.ProposalBlockParts.Header()

	// prevote arrives from vs2:
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlockHash, propPartSetHeader, vs2)
	ensurePrevote(t, voteCh, height, round) // prevote

	ensurePrecommit(t, voteCh, height, round) // precommit
	// the proposed block should now be locked and our precommit added
	validatePrecommit(ctx, t, cs1, 0, 0, vss[0], propBlockHash, propBlockHash)

	// we should be stuck in limbo waiting for more precommits

	// precommit arrives from vs2:
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, propBlockHash, propPartSetHeader, vs2)
	ensurePrecommit(t, voteCh, height, round)

	// wait to finish commit, propose in next height
	ensureNewBlock(t, newBlockCh, height)
}

//------------------------------------------------------------------------------------------
// LockSuite

// two validators, 4 rounds.
// two vals take turns proposing. val1 locks on first one, precommits nil on everything else
func TestStateLockNoPOL(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 2)
	vs2 := vss[1]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	timeoutProposeCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	voteCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryVote)
	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)

	/*
		Round1 (cs1, B) // B B // B B2
	*/

	// start round and wait for prevote
	cs1.enterNewRound(ctx, height, round)
	cs1.startRoutines(ctx, 0)

	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, proposalCh, height, round)
	roundState := cs1.GetRoundState()
	theBlockHash := roundState.ProposalBlock.Hash()
	thePartSetHeader := roundState.ProposalBlockParts.Header()

	ensurePrevote(t, voteCh, height, round) // prevote

	// we should now be stuck in limbo forever, waiting for more prevotes
	// prevote arrives from vs2:
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, theBlockHash, thePartSetHeader, vs2)
	ensurePrevote(t, voteCh, height, round) // prevote

	ensurePrecommit(t, voteCh, height, round) // precommit
	// the proposed block should now be locked and our precommit added
	validatePrecommit(ctx, t, cs1, round, round, vss[0], theBlockHash, theBlockHash)

	// we should now be stuck in limbo forever, waiting for more precommits
	// lets add one for a different block
	hash := make([]byte, len(theBlockHash))
	copy(hash, theBlockHash)
	hash[0] = (hash[0] + 1) % 255
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, hash, thePartSetHeader, vs2)
	ensurePrecommit(t, voteCh, height, round) // precommit

	// (note we're entering precommit for a second time this round)
	// but with invalid args. then we enterPrecommitWait, and the timeout to new round
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	///

	round++ // moving to the next round
	ensureNewRound(t, newRoundCh, height, round)
	t.Log("#### ONTO ROUND 1")
	/*
		Round2 (cs1, B) // B B2
	*/

	incrementRound(vs2)

	// now we're on a new round and not the proposer, so wait for timeout
	ensureNewTimeout(t, timeoutProposeCh, height, round, cs1.config.Propose(round).Nanoseconds())

	rs := cs1.GetRoundState()

	require.Nil(t, rs.ProposalBlock, "Expected proposal block to be nil")

	// wait to finish prevote
	ensurePrevote(t, voteCh, height, round)
	// we should have prevoted our locked block
	validatePrevote(ctx, t, cs1, round, vss[0], rs.LockedBlock.Hash())

	// add a conflicting prevote from the other validator
	bps, err := rs.LockedBlock.MakePartSet(partSize)
	require.NoError(t, err)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, hash, bps.Header(), vs2)
	ensurePrevote(t, voteCh, height, round)

	// now we're going to enter prevote again, but with invalid args
	// and then prevote wait, which should timeout. then wait for precommit
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Prevote(round).Nanoseconds())

	ensurePrecommit(t, voteCh, height, round) // precommit
	// the proposed block should still be locked and our precommit added
	// we should precommit nil and be locked on the proposal
	validatePrecommit(ctx, t, cs1, round, 0, vss[0], nil, theBlockHash)

	// add conflicting precommit from vs2
	bps2, err := rs.LockedBlock.MakePartSet(partSize)
	require.NoError(t, err)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, hash, bps2.Header(), vs2)
	ensurePrecommit(t, voteCh, height, round)

	// (note we're entering precommit for a second time this round, but with invalid args
	// then we enterPrecommitWait and timeout into NewRound
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	round++ // entering new round
	ensureNewRound(t, newRoundCh, height, round)
	t.Log("#### ONTO ROUND 2")
	/*
		Round3 (vs2, _) // B, B2
	*/

	incrementRound(vs2)

	ensureNewProposal(t, proposalCh, height, round)
	rs = cs1.GetRoundState()

	// now we're on a new round and are the proposer
	require.True(t, bytes.Equal(rs.ProposalBlock.Hash(), rs.LockedBlock.Hash()),
		"Expected proposal block to be locked block. Got %v, Expected %v",
		rs.ProposalBlock,
		rs.LockedBlock)

	ensurePrevote(t, voteCh, height, round) // prevote
	validatePrevote(ctx, t, cs1, round, vss[0], rs.LockedBlock.Hash())

	bps0, err := rs.ProposalBlock.MakePartSet(partSize)
	require.NoError(t, err)
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, hash, bps0.Header(), vs2)
	ensurePrevote(t, voteCh, height, round)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Prevote(round).Nanoseconds())
	ensurePrecommit(t, voteCh, height, round) // precommit

	validatePrecommit(ctx, t, cs1, round, 0, vss[0], nil, theBlockHash) // precommit nil but be locked on proposal

	bps1, err := rs.ProposalBlock.MakePartSet(partSize)
	require.NoError(t, err)
	signAddVotes(
		ctx,
		t,
		config,
		cs1,
		tmproto.PrecommitType,
		hash,
		bps1.Header(),
		vs2) // NOTE: conflicting precommits at same height
	ensurePrecommit(t, voteCh, height, round)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	// needed so generated block is different than locked block
	cs2, _ := randState(ctx, t, config, log.TestingLogger(), 2)
	require.NoError(t, err)
	// before we time out into new round, set next proposal block
	prop, propBlock := decideProposal(ctx, t, cs2, vs2, vs2.Height, vs2.Round+1)
	if prop == nil || propBlock == nil {
		t.Fatal("Failed to create proposal block with vs2")
	}

	incrementRound(vs2)

	round++ // entering new round
	ensureNewRound(t, newRoundCh, height, round)
	t.Log("#### ONTO ROUND 3")
	/*
		Round4 (vs2, C) // B C // B C
	*/

	// now we're on a new round and not the proposer
	// so set the proposal block
	bps3, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, bps3, ""); err != nil {
		t.Fatal(err)
	}

	ensureNewProposal(t, proposalCh, height, round)
	ensurePrevote(t, voteCh, height, round) // prevote
	// prevote for locked block (not proposal)
	validatePrevote(ctx, t, cs1, 3, vss[0], cs1.LockedBlock.Hash())

	// prevote for proposed block
	bps4, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlock.Hash(), bps4.Header(), vs2)
	ensurePrevote(t, voteCh, height, round)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Prevote(round).Nanoseconds())
	ensurePrecommit(t, voteCh, height, round)
	validatePrecommit(ctx, t, cs1, round, 0, vss[0], nil, theBlockHash) // precommit nil but locked on proposal

	bps5, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	signAddVotes(
		ctx,
		t,
		config,
		cs1,
		tmproto.PrecommitType,
		propBlock.Hash(),
		bps5.Header(),
		vs2) // NOTE: conflicting precommits at same height
	ensurePrecommit(t, voteCh, height, round)
}

// 4 vals in two rounds,
// in round one: v1 precommits, other 3 only prevote so the block isn't committed
// in round two: v1 prevotes the same block that the node is locked on
// the others prevote a new block hence v1 changes lock and precommits the new block with the others
func TestStateLockPOLRelock(t *testing.T) {
	config := configSetup(t)
	logger := log.TestingLogger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, logger, 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	newBlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewBlockHeader)

	// everything done from perspective of cs1

	/*
		Round1 (cs1, B) // B B B B// B nil B nil

		eg. vs2 and vs4 didn't see the 2/3 prevotes
	*/

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, height, round)

	ensureNewRound(t, newRoundCh, height, round)
	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	theBlockHash := rs.ProposalBlock.Hash()
	theBlockParts := rs.ProposalBlockParts.Header()

	ensurePrevote(t, voteCh, height, round) // prevote

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, theBlockHash, theBlockParts, vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round) // our precommit
	// the proposed block should now be locked and our precommit added
	validatePrecommit(ctx, t, cs1, round, round, vss[0], theBlockHash, theBlockHash)

	// add precommits from the rest
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	// before we timeout to the new round set the new proposal
	cs2 := newState(ctx, t, logger, cs1.state, vs2, kvstore.NewApplication())

	prop, propBlock := decideProposal(ctx, t, cs2, vs2, vs2.Height, vs2.Round+1)
	if prop == nil || propBlock == nil {
		t.Fatal("Failed to create proposal block with vs2")
	}
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	propBlockHash := propBlock.Hash()
	require.NotEqual(t, propBlockHash, theBlockHash)

	incrementRound(vs2, vs3, vs4)

	// timeout to new round
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	round++ // moving to the next round
	//XXX: this isnt guaranteed to get there before the timeoutPropose ...
	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}

	ensureNewRound(t, newRoundCh, height, round)
	t.Log("### ONTO ROUND 1")

	/*
		Round2 (vs2, C) // B C C C // C C C _)

		cs1 changes lock!
	*/

	// now we're on a new round and not the proposer
	// but we should receive the proposal
	ensureNewProposal(t, proposalCh, height, round)

	// go to prevote, node should prevote for locked block (not the new proposal) - this is relocking
	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], theBlockHash)

	// now lets add prevotes from everyone else for the new block
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlockHash, propBlockParts.Header(), vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// we should have unlocked and locked on the new block, sending a precommit for this new block
	validatePrecommit(ctx, t, cs1, round, round, vss[0], propBlockHash, propBlockHash)

	// more prevote creating a majority on the new block and this is then committed
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, propBlockHash, propBlockParts.Header(), vs2, vs3)
	ensureNewBlockHeader(t, newBlockCh, height, propBlockHash)

	ensureNewRound(t, newRoundCh, height+1, 0)
}

// 4 vals, one precommits, other 3 polka at next round, so we unlock and precomit the polka
func TestStateLockPOLUnlock(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	unlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryUnlock)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// everything done from perspective of cs1

	/*
		Round1 (cs1, B) // B B B B // B nil B nil
		eg. didn't see the 2/3 prevotes
	*/

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	theBlockHash := rs.ProposalBlock.Hash()
	theBlockParts := rs.ProposalBlockParts.Header()

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], theBlockHash)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, theBlockHash, theBlockParts, vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// the proposed block should now be locked and our precommit added
	validatePrecommit(ctx, t, cs1, round, round, vss[0], theBlockHash, theBlockHash)

	// add precommits from the rest
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs4)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, theBlockHash, theBlockParts, vs3)

	// before we time out into new round, set next proposal block
	prop, propBlock := decideProposal(ctx, t, cs1, vs2, vs2.Height, vs2.Round+1)
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	// timeout to new round
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())
	rs = cs1.GetRoundState()
	lockedBlockHash := rs.LockedBlock.Hash()

	incrementRound(vs2, vs3, vs4)
	round++ // moving to the next round

	ensureNewRound(t, newRoundCh, height, round)
	t.Log("#### ONTO ROUND 1")
	/*
		Round2 (vs2, C) // B nil nil nil // nil nil nil _
		cs1 unlocks!
	*/
	//XXX: this isnt guaranteed to get there before the timeoutPropose ...
	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}

	ensureNewProposal(t, proposalCh, height, round)

	// go to prevote, prevote for locked block (not proposal)
	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], lockedBlockHash)
	// now lets add prevotes from everyone else for nil (a polka!)
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	// the polka makes us unlock and precommit nil
	ensureNewUnlock(t, unlockCh, height, round)
	ensurePrecommit(t, voteCh, height, round)

	// we should have unlocked and committed nil
	// NOTE: since we don't relock on nil, the lock round is -1
	validatePrecommit(ctx, t, cs1, round, -1, vss[0], nil, nil)

	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3)
	ensureNewRound(t, newRoundCh, height, round+1)
}

// 4 vals, v1 locks on proposed block in the first round but the other validators only prevote
// In the second round, v1 misses the proposal but sees a majority prevote an unknown block so
// v1 should unlock and precommit nil. In the third round another block is proposed, all vals
// prevote and now v1 can lock onto the third block and precommit that
func TestStateLockPOLUnlockOnUnknownBlock(t *testing.T) {
	config := configSetup(t)
	logger := log.TestingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, logger, 4)

	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	// everything done from perspective of cs1

	/*
		Round0 (cs1, A) // A A A A// A nil nil nil
	*/

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, height, round)

	ensureNewRound(t, newRoundCh, height, round)
	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	firstBlockHash := rs.ProposalBlock.Hash()
	firstBlockParts := rs.ProposalBlockParts.Header()

	ensurePrevote(t, voteCh, height, round) // prevote

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, firstBlockHash, firstBlockParts, vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round) // our precommit
	// the proposed block should now be locked and our precommit added
	validatePrecommit(ctx, t, cs1, round, round, vss[0], firstBlockHash, firstBlockHash)

	// add precommits from the rest
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	// before we timeout to the new round set the new proposal
	cs2 := newState(ctx, t, logger, cs1.state, vs2, kvstore.NewApplication())
	prop, propBlock := decideProposal(ctx, t, cs2, vs2, vs2.Height, vs2.Round+1)
	if prop == nil || propBlock == nil {
		t.Fatal("Failed to create proposal block with vs2")
	}
	secondBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	secondBlockHash := propBlock.Hash()
	require.NotEqual(t, secondBlockHash, firstBlockHash)

	incrementRound(vs2, vs3, vs4)

	// timeout to new round
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	round++ // moving to the next round

	ensureNewRound(t, newRoundCh, height, round)
	t.Log("### ONTO ROUND 1")

	/*
		Round1 (vs2, B) // A B B B // nil nil nil nil)
	*/

	// now we're on a new round but v1 misses the proposal

	// go to prevote, node should prevote for locked block (not the new proposal) - this is relocking
	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], firstBlockHash)

	// now lets add prevotes from everyone else for the new block
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, secondBlockHash, secondBlockParts.Header(), vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// we should have unlocked and locked on the new block, sending a precommit for this new block
	validatePrecommit(ctx, t, cs1, round, -1, vss[0], nil, nil)

	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, secondBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}

	// more prevote creating a majority on the new block and this is then committed
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	// before we timeout to the new round set the new proposal
	cs3 := newState(ctx, t, logger, cs1.state, vs3, kvstore.NewApplication())
	require.NoError(t, err)
	prop, propBlock = decideProposal(ctx, t, cs3, vs3, vs3.Height, vs3.Round+1)
	if prop == nil || propBlock == nil {
		t.Fatal("Failed to create proposal block with vs2")
	}
	thirdPropBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	thirdPropBlockHash := propBlock.Hash()
	require.NotEqual(t, secondBlockHash, thirdPropBlockHash)

	incrementRound(vs2, vs3, vs4)

	// timeout to new round
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	round++ // moving to the next round
	ensureNewRound(t, newRoundCh, height, round)
	t.Log("### ONTO ROUND 2")

	/*
		Round2 (vs3, C) // C C C C // C nil nil nil)
	*/

	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, thirdPropBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}

	ensurePrevote(t, voteCh, height, round)
	// we are no longer locked to the first block so we should be able to prevote
	validatePrevote(ctx, t, cs1, round, vss[0], thirdPropBlockHash)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, thirdPropBlockHash, thirdPropBlockParts.Header(), vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// we have a majority, now vs1 can change lock to the third block
	validatePrecommit(ctx, t, cs1, round, round, vss[0], thirdPropBlockHash, thirdPropBlockHash)
}

// 4 vals
// a polka at round 1 but we miss it
// then a polka at round 2 that we lock on
// then we see the polka from round 1 but shouldn't unlock
func TestStateLockPOLSafety1(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutProposeCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, cs1.Height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	propBlock := rs.ProposalBlock

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], propBlock.Hash())

	// the others sign a polka but we don't see it
	bps, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	prevotes := signVotes(ctx, t, config, tmproto.PrevoteType,
		propBlock.Hash(), bps.Header(),
		vs2, vs3, vs4)

	t.Logf("old prop hash %v", fmt.Sprintf("%X", propBlock.Hash()))

	// we do see them precommit nil
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	// cs1 precommit nil
	ensurePrecommit(t, voteCh, height, round)
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	t.Log("### ONTO ROUND 1")

	prop, propBlock := decideProposal(ctx, t, cs1, vs2, vs2.Height, vs2.Round+1)
	propBlockHash := propBlock.Hash()
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	incrementRound(vs2, vs3, vs4)

	round++ // moving to the next round
	ensureNewRound(t, newRoundCh, height, round)

	//XXX: this isnt guaranteed to get there before the timeoutPropose ...
	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}
	/*Round2
	// we timeout and prevote our lock
	// a polka happened but we didn't see it!
	*/

	ensureNewProposal(t, proposalCh, height, round)

	rs = cs1.GetRoundState()

	require.Nil(t, rs.LockedBlock, "we should not be locked!")

	t.Logf("new prop hash %v", fmt.Sprintf("%X", propBlockHash))

	// go to prevote, prevote for proposal block
	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], propBlockHash)

	// now we see the others prevote for it, so we should lock on it
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlockHash, propBlockParts.Header(), vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// we should have precommitted
	validatePrecommit(ctx, t, cs1, round, round, vss[0], propBlockHash, propBlockHash)

	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	incrementRound(vs2, vs3, vs4)
	round++ // moving to the next round

	ensureNewRound(t, newRoundCh, height, round)

	t.Log("### ONTO ROUND 2")
	/*Round3
	we see the polka from round 1 but we shouldn't unlock!
	*/

	// timeout of propose
	ensureNewTimeout(t, timeoutProposeCh, height, round, cs1.config.Propose(round).Nanoseconds())

	// finish prevote
	ensurePrevote(t, voteCh, height, round)
	// we should prevote what we're locked on
	validatePrevote(ctx, t, cs1, round, vss[0], propBlockHash)

	newStepCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRoundStep)

	// before prevotes from the previous round are added
	// add prevotes from the earlier round
	addVotes(cs1, prevotes...)

	t.Log("Done adding prevotes!")

	ensureNoNewRoundStep(t, newStepCh)
}

// 4 vals.
// polka P0 at R0, P1 at R1, and P2 at R2,
// we lock on P0 at R0, don't see P1, and unlock using P2 at R2
// then we should make sure we don't lock using P1

// What we want:
// dont see P0, lock on P1 at R1, dont unlock using P0 at R2
func TestStateLockPOLSafety2(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	unlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryUnlock)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// the block for R0: gets polkad but we miss it
	// (even though we signed it, shhh)
	_, propBlock0 := decideProposal(ctx, t, cs1, vss[0], height, round)
	propBlockHash0 := propBlock0.Hash()
	propBlockParts0, err := propBlock0.MakePartSet(partSize)
	require.NoError(t, err)
	propBlockID0 := types.BlockID{Hash: propBlockHash0, PartSetHeader: propBlockParts0.Header()}

	// the others sign a polka but we don't see it
	prevotes := signVotes(ctx, t, config, tmproto.PrevoteType, propBlockHash0, propBlockParts0.Header(), vs2, vs3, vs4)

	// the block for round 1
	prop1, propBlock1 := decideProposal(ctx, t, cs1, vs2, vs2.Height, vs2.Round+1)
	propBlockHash1 := propBlock1.Hash()
	propBlockParts1, err := propBlock1.MakePartSet(partSize)
	require.NoError(t, err)

	incrementRound(vs2, vs3, vs4)

	round++ // moving to the next round
	t.Log("### ONTO Round 1")
	// jump in at round 1
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	if err := cs1.SetProposalAndBlock(ctx, prop1, propBlock1, propBlockParts1, "some peer"); err != nil {
		t.Fatal(err)
	}
	ensureNewProposal(t, proposalCh, height, round)

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], propBlockHash1)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlockHash1, propBlockParts1.Header(), vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// the proposed block should now be locked and our precommit added
	validatePrecommit(ctx, t, cs1, round, round, vss[0], propBlockHash1, propBlockHash1)

	// add precommits from the rest
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs4)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, propBlockHash1, propBlockParts1.Header(), vs3)

	incrementRound(vs2, vs3, vs4)

	// timeout of precommit wait to new round
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	round++ // moving to the next round
	// in round 2 we see the polkad block from round 0
	newProp := types.NewProposal(height, round, 0, propBlockID0)
	p := newProp.ToProto()
	if err := vs3.SignProposal(ctx, config.ChainID(), p); err != nil {
		t.Fatal(err)
	}

	newProp.Signature = p.Signature

	if err := cs1.SetProposalAndBlock(ctx, newProp, propBlock0, propBlockParts0, "some peer"); err != nil {
		t.Fatal(err)
	}

	// Add the pol votes
	addVotes(cs1, prevotes...)

	ensureNewRound(t, newRoundCh, height, round)
	t.Log("### ONTO Round 2")
	/*Round2
	// now we see the polka from round 1, but we shouldnt unlock
	*/
	ensureNewProposal(t, proposalCh, height, round)

	ensureNoNewUnlock(t, unlockCh)
	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], propBlockHash1)

}

// 4 vals.
// polka P0 at R0 for B0. We lock B0 on P0 at R0. P0 unlocks value at R1.

// What we want:
// P0 proposes B0 at R3.
func TestProposeValidBlock(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	timeoutProposeCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	unlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryUnlock)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, cs1.Height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	propBlock := rs.ProposalBlock
	propBlockHash := propBlock.Hash()

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], propBlockHash)

	// the others sign a polka
	bps, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType,
		propBlockHash, bps.Header(), vs2,
		vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// we should have precommitted
	validatePrecommit(ctx, t, cs1, round, round, vss[0], propBlockHash, propBlockHash)

	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	incrementRound(vs2, vs3, vs4)
	round++ // moving to the next round

	ensureNewRound(t, newRoundCh, height, round)

	t.Log("### ONTO ROUND 2")

	// timeout of propose
	ensureNewTimeout(t, timeoutProposeCh, height, round, cs1.config.Propose(round).Nanoseconds())

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], propBlockHash)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	ensureNewUnlock(t, unlockCh, height, round)

	ensurePrecommit(t, voteCh, height, round)
	// we should have precommitted
	validatePrecommit(ctx, t, cs1, round, -1, vss[0], nil, nil)

	incrementRound(vs2, vs3, vs4)
	incrementRound(vs2, vs3, vs4)

	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	round += 2 // moving to the next round

	ensureNewRound(t, newRoundCh, height, round)
	t.Log("### ONTO ROUND 3")

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	round++ // moving to the next round

	ensureNewRound(t, newRoundCh, height, round)

	t.Log("### ONTO ROUND 4")

	ensureNewProposal(t, proposalCh, height, round)

	rs = cs1.GetRoundState()
	assert.True(t, bytes.Equal(rs.ProposalBlock.Hash(), propBlockHash))
	assert.True(t, bytes.Equal(rs.ProposalBlock.Hash(), rs.ValidBlock.Hash()))
	assert.True(t, rs.Proposal.POLRound == rs.ValidRound)
	assert.True(t, bytes.Equal(rs.Proposal.BlockID.Hash, rs.ValidBlock.Hash()))
}

// What we want:
// P0 miss to lock B but set valid block to B after receiving delayed prevote.
func TestSetValidBlockOnDelayedPrevote(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	validBlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryValidBlock)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, cs1.Height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	propBlock := rs.ProposalBlock
	propBlockHash := propBlock.Hash()
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], propBlockHash)

	// vs2 send prevote for propBlock
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlockHash, propBlockParts.Header(), vs2)

	// vs3 send prevote nil
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, nil, types.PartSetHeader{}, vs3)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Prevote(round).Nanoseconds())

	ensurePrecommit(t, voteCh, height, round)
	// we should have precommitted
	validatePrecommit(ctx, t, cs1, round, -1, vss[0], nil, nil)

	rs = cs1.GetRoundState()

	assert.True(t, rs.ValidBlock == nil)
	assert.True(t, rs.ValidBlockParts == nil)
	assert.True(t, rs.ValidRound == -1)

	// vs2 send (delayed) prevote for propBlock
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlockHash, propBlockParts.Header(), vs4)

	ensureNewValidBlock(t, validBlockCh, height, round)

	rs = cs1.GetRoundState()

	assert.True(t, bytes.Equal(rs.ValidBlock.Hash(), propBlockHash))
	assert.True(t, rs.ValidBlockParts.Header().Equals(propBlockParts.Header()))
	assert.True(t, rs.ValidRound == round)
}

// What we want:
// P0 miss to lock B as Proposal Block is missing, but set valid block to B after
// receiving delayed Block Proposal.
func TestSetValidBlockOnDelayedProposal(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	timeoutProposeCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	validBlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryValidBlock)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)
	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)

	round++ // move to round in which P0 is not proposer
	incrementRound(vs2, vs3, vs4)

	startTestRound(ctx, cs1, cs1.Height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensureNewTimeout(t, timeoutProposeCh, height, round, cs1.config.Propose(round).Nanoseconds())

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], nil)

	prop, propBlock := decideProposal(ctx, t, cs1, vs2, vs2.Height, vs2.Round+1)
	propBlockHash := propBlock.Hash()
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	// vs2, vs3 and vs4 send prevote for propBlock
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlockHash, propBlockParts.Header(), vs2, vs3, vs4)
	ensureNewValidBlock(t, validBlockCh, height, round)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Prevote(round).Nanoseconds())

	ensurePrecommit(t, voteCh, height, round)
	validatePrecommit(ctx, t, cs1, round, -1, vss[0], nil, nil)

	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}

	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()

	assert.True(t, bytes.Equal(rs.ValidBlock.Hash(), propBlockHash))
	assert.True(t, rs.ValidBlockParts.Header().Equals(propBlockParts.Header()))
	assert.True(t, rs.ValidRound == round)
}

// 4 vals, 3 Nil Precommits at P0
// What we want:
// P0 waits for timeoutPrecommit before starting next round
func TestWaitingTimeoutOnNilPolka(t *testing.T) {
	config := configSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)

	// start round
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())
	ensureNewRound(t, newRoundCh, height, round+1)
}

// 4 vals, 3 Prevotes for nil from the higher round.
// What we want:
// P0 waits for timeoutPropose in the next round before entering prevote
func TestWaitingTimeoutProposeOnNewRound(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensurePrevote(t, voteCh, height, round)

	incrementRound(vss[1:]...)
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	round++ // moving to the next round
	ensureNewRound(t, newRoundCh, height, round)

	rs := cs1.GetRoundState()
	assert.True(t, rs.Step == cstypes.RoundStepPropose) // P0 does not prevote before timeoutPropose expires

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Propose(round).Nanoseconds())

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], nil)
}

// 4 vals, 3 Precommits for nil from the higher round.
// What we want:
// P0 jump to higher round, precommit and start precommit wait
func TestRoundSkipOnNilPolkaFromHigherRound(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensurePrevote(t, voteCh, height, round)

	incrementRound(vss[1:]...)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	round++ // moving to the next round
	ensureNewRound(t, newRoundCh, height, round)

	ensurePrecommit(t, voteCh, height, round)
	validatePrecommit(ctx, t, cs1, round, -1, vss[0], nil, nil)

	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	round++ // moving to the next round
	ensureNewRound(t, newRoundCh, height, round)
}

// 4 vals, 3 Prevotes for nil in the current round.
// What we want:
// P0 wait for timeoutPropose to expire before sending prevote.
func TestWaitTimeoutProposeOnNilPolkaForTheCurrentRound(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)

	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, int32(1)

	timeoutProposeCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round in which PO is not proposer
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	incrementRound(vss[1:]...)
	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, nil, types.PartSetHeader{}, vs2, vs3, vs4)

	ensureNewTimeout(t, timeoutProposeCh, height, round, cs1.config.Propose(round).Nanoseconds())

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], nil)
}

// What we want:
// P0 emit NewValidBlock event upon receiving 2/3+ Precommit for B but hasn't received block B yet
func TestEmitNewValidBlockEventOnCommitWithoutBlock(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, int32(1)

	incrementRound(vs2, vs3, vs4)

	partSize := types.BlockPartSizeBytes

	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	validBlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryValidBlock)

	_, propBlock := decideProposal(ctx, t, cs1, vs2, vs2.Height, vs2.Round)
	propBlockHash := propBlock.Hash()
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	// start round in which PO is not proposer
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	// vs2, vs3 and vs4 send precommit for propBlock
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, propBlockHash, propBlockParts.Header(), vs2, vs3, vs4)
	ensureNewValidBlock(t, validBlockCh, height, round)

	rs := cs1.GetRoundState()
	assert.True(t, rs.Step == cstypes.RoundStepCommit)
	assert.True(t, rs.ProposalBlock == nil)
	assert.True(t, rs.ProposalBlockParts.Header().Equals(propBlockParts.Header()))

}

// What we want:
// P0 receives 2/3+ Precommit for B for round 0, while being in round 1. It emits NewValidBlock event.
// After receiving block, it executes block and moves to the next height.
func TestCommitFromPreviousRound(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, int32(1)

	partSize := types.BlockPartSizeBytes

	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	validBlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryValidBlock)
	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)

	prop, propBlock := decideProposal(ctx, t, cs1, vs2, vs2.Height, vs2.Round)
	propBlockHash := propBlock.Hash()
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	// start round in which PO is not proposer
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	// vs2, vs3 and vs4 send precommit for propBlock for the previous round
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, propBlockHash, propBlockParts.Header(), vs2, vs3, vs4)

	ensureNewValidBlock(t, validBlockCh, height, round)

	rs := cs1.GetRoundState()
	assert.True(t, rs.Step == cstypes.RoundStepCommit)
	assert.True(t, rs.CommitRound == vs2.Round)
	assert.True(t, rs.ProposalBlock == nil)
	assert.True(t, rs.ProposalBlockParts.Header().Equals(propBlockParts.Header()))

	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}

	ensureNewProposal(t, proposalCh, height, round)
	ensureNewRound(t, newRoundCh, height+1, 0)
}

type fakeTxNotifier struct {
	ch chan struct{}
}

func (n *fakeTxNotifier) TxsAvailable() <-chan struct{} {
	return n.ch
}

func (n *fakeTxNotifier) Notify() {
	n.ch <- struct{}{}
}

// 2 vals precommit votes for a block but node times out waiting for the third. Move to next round
// and third precommit arrives which leads to the commit of that header and the correct
// start of the next round
func TestStartNextHeightCorrectlyAfterTimeout(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config.Consensus.SkipTimeoutCommit = false
	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	cs1.txNotifier = &fakeTxNotifier{ch: make(chan struct{})}

	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutProposeCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)
	precommitTimeoutCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)

	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	newBlockHeader := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewBlockHeader)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	theBlockHash := rs.ProposalBlock.Hash()
	theBlockParts := rs.ProposalBlockParts.Header()

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], theBlockHash)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, theBlockHash, theBlockParts, vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// the proposed block should now be locked and our precommit added
	validatePrecommit(ctx, t, cs1, round, round, vss[0], theBlockHash, theBlockHash)

	// add precommits
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, theBlockHash, theBlockParts, vs3)

	// wait till timeout occurs
	ensurePrecommitTimeout(t, precommitTimeoutCh)

	ensureNewRound(t, newRoundCh, height, round+1)

	// majority is now reached
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, theBlockHash, theBlockParts, vs4)

	ensureNewBlockHeader(t, newBlockHeader, height, theBlockHash)

	cs1.txNotifier.(*fakeTxNotifier).Notify()

	ensureNewTimeout(t, timeoutProposeCh, height+1, round, cs1.config.Propose(round).Nanoseconds())
	rs = cs1.GetRoundState()
	assert.False(
		t,
		rs.TriggeredTimeoutPrecommit,
		"triggeredTimeoutPrecommit should be false at the beginning of each round")
}

func TestResetTimeoutPrecommitUponNewHeight(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config.Consensus.SkipTimeoutCommit = false
	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)

	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round

	partSize := types.BlockPartSizeBytes

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)

	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	newBlockHeader := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewBlockHeader)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	theBlockHash := rs.ProposalBlock.Hash()
	theBlockParts := rs.ProposalBlockParts.Header()

	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], theBlockHash)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, theBlockHash, theBlockParts, vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	validatePrecommit(ctx, t, cs1, round, round, vss[0], theBlockHash, theBlockHash)

	// add precommits
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, theBlockHash, theBlockParts, vs3)
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, theBlockHash, theBlockParts, vs4)

	ensureNewBlockHeader(t, newBlockHeader, height, theBlockHash)

	prop, propBlock := decideProposal(ctx, t, cs1, vs2, height+1, 0)
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	if err := cs1.SetProposalAndBlock(ctx, prop, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}
	ensureNewProposal(t, proposalCh, height+1, 0)

	rs = cs1.GetRoundState()
	assert.False(
		t,
		rs.TriggeredTimeoutPrecommit,
		"triggeredTimeoutPrecommit should be false at the beginning of each height")
}

//------------------------------------------------------------------------------------------
// SlashingSuite
// TODO: Slashing

/*
func TestStateSlashingPrevotes(t *testing.T) {
	cs1, vss := randState(2)
	vs2 := vss[1]


	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	voteCh := subscribeToVoter(ctx, t, cs1, cs1.privValidator.GetAddress())

	// start round and wait for propose and prevote
	startTestRound(cs1, cs1.Height, 0)
	<-newRoundCh
	re := <-proposalCh
	<-voteCh // prevote

	rs := re.(types.EventDataRoundState).RoundState.(*cstypes.RoundState)

	// we should now be stuck in limbo forever, waiting for more prevotes
	// add one for a different block should cause us to go into prevote wait
	hash := rs.ProposalBlock.Hash()
	hash[0] = byte(hash[0]+1) % 255
	signAddVotes(cs1, tmproto.PrevoteType, hash, rs.ProposalBlockParts.Header(), vs2)

	<-timeoutWaitCh

	// NOTE: we have to send the vote for different block first so we don't just go into precommit round right
	// away and ignore more prevotes (and thus fail to slash!)

	// add the conflicting vote
	signAddVotes(cs1, tmproto.PrevoteType, rs.ProposalBlock.Hash(), rs.ProposalBlockParts.Header(), vs2)

	// XXX: Check for existence of Dupeout info
}

func TestStateSlashingPrecommits(t *testing.T) {
	cs1, vss := randState(2)
	vs2 := vss[1]


	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	voteCh := subscribeToVoter(ctx, t, cs1, cs1.privValidator.GetAddress())

	// start round and wait for propose and prevote
	startTestRound(cs1, cs1.Height, 0)
	<-newRoundCh
	re := <-proposalCh
	<-voteCh // prevote

	// add prevote from vs2
	signAddVotes(cs1, tmproto.PrevoteType, rs.ProposalBlock.Hash(), rs.ProposalBlockParts.Header(), vs2)

	<-voteCh // precommit

	// we should now be stuck in limbo forever, waiting for more prevotes
	// add one for a different block should cause us to go into prevote wait
	hash := rs.ProposalBlock.Hash()
	hash[0] = byte(hash[0]+1) % 255
	signAddVotes(cs1, tmproto.PrecommitType, hash, rs.ProposalBlockParts.Header(), vs2)

	// NOTE: we have to send the vote for different block first so we don't just go into precommit round right
	// away and ignore more prevotes (and thus fail to slash!)

	// add precommit from vs2
	signAddVotes(cs1, tmproto.PrecommitType, rs.ProposalBlock.Hash(), rs.ProposalBlockParts.Header(), vs2)

	// XXX: Check for existence of Dupeout info
}
*/

//------------------------------------------------------------------------------------------
// CatchupSuite

//------------------------------------------------------------------------------------------
// HaltSuite

// 4 vals.
// we receive a final precommit after going into next round, but others might have gone to commit already!
func TestStateHalt1(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs1, vss := randState(ctx, t, config, log.TestingLogger(), 4)
	vs2, vs3, vs4 := vss[1], vss[2], vss[3]
	height, round := cs1.Height, cs1.Round
	partSize := types.BlockPartSizeBytes

	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutWaitCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutWait)
	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	newBlockCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewBlock)
	pv1, err := cs1.privValidator.GetPubKey(ctx)
	require.NoError(t, err)
	addr := pv1.Address()
	voteCh := subscribeToVoter(ctx, t, cs1, addr)

	// start round and wait for propose and prevote
	startTestRound(ctx, cs1, height, round)
	ensureNewRound(t, newRoundCh, height, round)

	ensureNewProposal(t, proposalCh, height, round)
	rs := cs1.GetRoundState()
	propBlock := rs.ProposalBlock
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)

	ensurePrevote(t, voteCh, height, round)

	signAddVotes(ctx, t, config, cs1, tmproto.PrevoteType, propBlock.Hash(), propBlockParts.Header(), vs2, vs3, vs4)

	ensurePrecommit(t, voteCh, height, round)
	// the proposed block should now be locked and our precommit added
	validatePrecommit(ctx, t, cs1, round, round, vss[0], propBlock.Hash(), propBlock.Hash())

	// add precommits from the rest
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, nil, types.PartSetHeader{}, vs2) // didnt receive proposal
	signAddVotes(ctx, t, config, cs1, tmproto.PrecommitType, propBlock.Hash(), propBlockParts.Header(), vs3)
	// we receive this later, but vs3 might receive it earlier and with ours will go to commit!
	precommit4 := signVote(ctx, t, vs4, config, tmproto.PrecommitType, propBlock.Hash(), propBlockParts.Header())

	incrementRound(vs2, vs3, vs4)

	// timeout to new round
	ensureNewTimeout(t, timeoutWaitCh, height, round, cs1.config.Precommit(round).Nanoseconds())

	round++ // moving to the next round

	ensureNewRound(t, newRoundCh, height, round)
	rs = cs1.GetRoundState()

	t.Log("### ONTO ROUND 1")
	/*Round2
	// we timeout and prevote our lock
	// a polka happened but we didn't see it!
	*/

	// go to prevote, prevote for locked block
	ensurePrevote(t, voteCh, height, round)
	validatePrevote(ctx, t, cs1, round, vss[0], rs.LockedBlock.Hash())

	// now we receive the precommit from the previous round
	addVotes(cs1, precommit4)

	// receiving that precommit should take us straight to commit
	ensureNewBlock(t, newBlockCh, height)

	ensureNewRound(t, newRoundCh, height+1, 0)
}

func TestStateOutputsBlockPartsStats(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// create dummy peer
	cs, _ := randState(ctx, t, config, log.TestingLogger(), 1)
	peerID, err := types.NewNodeID("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	require.NoError(t, err)

	// 1) new block part
	parts := types.NewPartSetFromData(tmrand.Bytes(100), 10)
	msg := &BlockPartMessage{
		Height: 1,
		Round:  0,
		Part:   parts.GetPart(0),
	}

	cs.ProposalBlockParts = types.NewPartSetFromHeader(parts.Header())
	cs.handleMsg(ctx, msgInfo{msg, peerID})

	statsMessage := <-cs.statsMsgQueue
	require.Equal(t, msg, statsMessage.Msg, "")
	require.Equal(t, peerID, statsMessage.PeerID, "")

	// sending the same part from different peer
	cs.handleMsg(ctx, msgInfo{msg, "peer2"})

	// sending the part with the same height, but different round
	msg.Round = 1
	cs.handleMsg(ctx, msgInfo{msg, peerID})

	// sending the part from the smaller height
	msg.Height = 0
	cs.handleMsg(ctx, msgInfo{msg, peerID})

	// sending the part from the bigger height
	msg.Height = 3
	cs.handleMsg(ctx, msgInfo{msg, peerID})

	select {
	case <-cs.statsMsgQueue:
		t.Errorf("should not output stats message after receiving the known block part!")
	case <-time.After(50 * time.Millisecond):
	}

}

func TestStateOutputVoteStats(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs, vss := randState(ctx, t, config, log.TestingLogger(), 2)
	// create dummy peer
	peerID, err := types.NewNodeID("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	require.NoError(t, err)

	randBytes := tmrand.Bytes(tmhash.Size)

	vote := signVote(ctx, t, vss[1], config, tmproto.PrecommitType, randBytes, types.PartSetHeader{})

	voteMessage := &VoteMessage{vote}
	cs.handleMsg(ctx, msgInfo{voteMessage, peerID})

	statsMessage := <-cs.statsMsgQueue
	require.Equal(t, voteMessage, statsMessage.Msg, "")
	require.Equal(t, peerID, statsMessage.PeerID, "")

	// sending the same part from different peer
	cs.handleMsg(ctx, msgInfo{&VoteMessage{vote}, "peer2"})

	// sending the vote for the bigger height
	incrementHeight(vss[1])
	vote = signVote(ctx, t, vss[1], config, tmproto.PrecommitType, randBytes, types.PartSetHeader{})

	cs.handleMsg(ctx, msgInfo{&VoteMessage{vote}, peerID})

	select {
	case <-cs.statsMsgQueue:
		t.Errorf("should not output stats message after receiving the known vote or vote from bigger height")
	case <-time.After(50 * time.Millisecond):
	}

}

func TestSignSameVoteTwice(t *testing.T) {
	config := configSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, vss := randState(ctx, t, config, log.TestingLogger(), 2)

	randBytes := tmrand.Bytes(tmhash.Size)

	vote := signVote(
		ctx,
		t,
		vss[1],
		config,
		tmproto.PrecommitType,
		randBytes,
		types.PartSetHeader{Total: 10, Hash: randBytes},
	)

	vote2 := signVote(
		ctx,
		t,
		vss[1],
		config,
		tmproto.PrecommitType,
		randBytes,
		types.PartSetHeader{Total: 10, Hash: randBytes},
	)

	require.Equal(t, vote, vote2)
}

// subscribe subscribes test client to the given query and returns a channel with cap = 1.
func subscribe(
	ctx context.Context,
	t *testing.T,
	eventBus *eventbus.EventBus,
	q tmpubsub.Query,
) <-chan tmpubsub.Message {
	t.Helper()
	sub, err := eventBus.SubscribeWithArgs(ctx, tmpubsub.SubscribeArgs{
		ClientID: testSubscriber,
		Query:    q,
	})
	if err != nil {
		t.Fatalf("Failed to subscribe %q to %v: %v", testSubscriber, q, err)
	}
	ch := make(chan tmpubsub.Message)
	go func() {
		for {
			next, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				t.Errorf("Subscription for %v unexpectedly terminated: %v", q, err)
				return
			}
			ch <- next
		}
	}()
	return ch
}
