(window.webpackJsonp=window.webpackJsonp||[]).push([[144],{693:function(e,t,s){"use strict";s.r(t);var n=s(1),a=Object(n.a)({},(function(){var e=this,t=e.$createElement,s=e._self._c||t;return s("ContentSlotsDistributor",{attrs:{"slot-key":e.$parent.slotKey}},[s("h1",{attrs:{id:"tendermint-spec"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#tendermint-spec"}},[e._v("#")]),e._v(" Tendermint Spec")]),e._v(" "),s("p",[e._v("This is a markdown specification of the Tendermint blockchain.\nIt defines the base data structures, how they are validated,\nand how they are communicated over the network.")]),e._v(" "),s("p",[e._v("If you find discrepancies between the spec and the code that\ndo not have an associated issue or pull request on github,\nplease submit them to our "),s("a",{attrs:{href:"https://tendermint.com/security",target:"_blank",rel:"noopener noreferrer"}},[e._v("bug bounty"),s("OutboundLink")],1),e._v("!")]),e._v(" "),s("h2",{attrs:{id:"contents"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#contents"}},[e._v("#")]),e._v(" Contents")]),e._v(" "),s("ul",[s("li",[s("a",{attrs:{href:"#overview"}},[e._v("Overview")])])]),e._v(" "),s("h3",{attrs:{id:"data-structures"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#data-structures"}},[e._v("#")]),e._v(" Data Structures")]),e._v(" "),s("ul",[s("li",[s("RouterLink",{attrs:{to:"/spec/core/encoding.html"}},[e._v("Encoding and Digests")])],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/core/data_structures.html"}},[e._v("Blockchain")])],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/core/state.html"}},[e._v("State")])],1)]),e._v(" "),s("h3",{attrs:{id:"consensus-protocol"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#consensus-protocol"}},[e._v("#")]),e._v(" Consensus Protocol")]),e._v(" "),s("ul",[s("li",[s("RouterLink",{attrs:{to:"/spec/consensus/consensus.html"}},[e._v("Consensus Algorithm")])],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/consensus/creating-proposal.html"}},[e._v("Creating a proposal")])],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/consensus/bft-time.html"}},[e._v("Time")])],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/consensus/light-client/"}},[e._v("Light-Client")])],1)]),e._v(" "),s("h3",{attrs:{id:"p2p-and-network-protocols"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#p2p-and-network-protocols"}},[e._v("#")]),e._v(" P2P and Network Protocols")]),e._v(" "),s("ul",[s("li",[s("RouterLink",{attrs:{to:"/spec/p2p/node.html"}},[e._v("The Base P2P Layer")]),e._v(': multiplex the protocols ("reactors") on authenticated and encrypted TCP connections')],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/p2p/messages/pex.html"}},[e._v("Peer Exchange (PEX)")]),e._v(": gossip known peer addresses so peers can find each other")],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/p2p/messages/block-sync.html"}},[e._v("Block Sync")]),e._v(": gossip blocks so peers can catch up quickly")],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/p2p/messages/consensus.html"}},[e._v("Consensus")]),e._v(": gossip votes and block parts so new blocks can be committed")],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/p2p/messages/mempool.html"}},[e._v("Mempool")]),e._v(": gossip transactions so they get included in blocks")],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/p2p/messages/evidence.html"}},[e._v("Evidence")]),e._v(": sending invalid evidence will stop the peer")],1)]),e._v(" "),s("h3",{attrs:{id:"software"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#software"}},[e._v("#")]),e._v(" Software")]),e._v(" "),s("ul",[s("li",[s("RouterLink",{attrs:{to:"/spec/abci/"}},[e._v("ABCI")]),e._v(": Details about interactions between the\napplication and consensus engine over ABCI")],1),e._v(" "),s("li",[s("RouterLink",{attrs:{to:"/spec/consensus/wal.html"}},[e._v("Write-Ahead Log")]),e._v(": Details about how the consensus\nengine preserves data and recovers from crash failures")],1)]),e._v(" "),s("h2",{attrs:{id:"overview"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#overview"}},[e._v("#")]),e._v(" Overview")]),e._v(" "),s("p",[e._v('Tendermint provides Byzantine Fault Tolerant State Machine Replication using\nhash-linked batches of transactions. Such transaction batches are called "blocks".\nHence, Tendermint defines a "blockchain".')]),e._v(" "),s("p",[e._v("Each block in Tendermint has a unique index - its Height.\nHeight's in the blockchain are monotonic.\nEach block is committed by a known set of weighted Validators.\nMembership and weighting within this validator set may change over time.\nTendermint guarantees the safety and liveness of the blockchain\nso long as less than 1/3 of the total weight of the Validator set\nis malicious or faulty.")]),e._v(" "),s("p",[e._v("A commit in Tendermint is a set of signed messages from more than 2/3 of\nthe total weight of the current Validator set. Validators take turns proposing\nblocks and voting on them. Once enough votes are received, the block is considered\ncommitted. These votes are included in the "),s("em",[e._v("next")]),e._v(" block as proof that the previous block\nwas committed - they cannot be included in the current block, as that block has already been\ncreated.")]),e._v(" "),s("p",[e._v("Once a block is committed, it can be executed against an application.\nThe application returns results for each of the transactions in the block.\nThe application can also return changes to be made to the validator set,\nas well as a cryptographic digest of its latest state.")]),e._v(" "),s("p",[e._v('Tendermint is designed to enable efficient verification and authentication\nof the latest state of the blockchain. To achieve this, it embeds\ncryptographic commitments to certain information in the block "header".\nThis information includes the contents of the block (eg. the transactions),\nthe validator set committing the block, as well as the various results returned by the application.\nNote, however, that block execution only occurs '),s("em",[e._v("after")]),e._v(" a block is committed.\nThus, application results can only be included in the "),s("em",[e._v("next")]),e._v(" block.")]),e._v(" "),s("p",[e._v("Also note that information like the transaction results and the validator set are never\ndirectly included in the block - only their cryptographic digests (Merkle roots) are.\nHence, verification of a block requires a separate data structure to store this information.\nWe call this the "),s("code",[e._v("State")]),e._v(". Block verification also requires access to the previous block.")])])}),[],!1,null,null,null);t.default=a.exports}}]);