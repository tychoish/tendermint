(window.webpackJsonp=window.webpackJsonp||[]).push([[220],{642:function(e,t,s){"use strict";s.r(t);var a=s(1),n=Object(a.a)({},(function(){var e=this,t=e.$createElement,s=e._self._c||t;return s("ContentSlotsDistributor",{attrs:{"slot-key":e.$parent.slotKey}},[s("h1",{attrs:{id:"tm-signer-harness"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#tm-signer-harness"}},[e._v("#")]),e._v(" tm-signer-harness")]),e._v(" "),s("p",[e._v("Located under the "),s("code",[e._v("tools/tm-signer-harness")]),e._v(" folder in the "),s("a",{attrs:{href:"https://github.com/tendermint/tendermint",target:"_blank",rel:"noopener noreferrer"}},[e._v("Tendermint\nrepository"),s("OutboundLink")],1),e._v(".")]),e._v(" "),s("p",[e._v("The Tendermint remote signer test harness facilitates integration testing\nbetween Tendermint and remote signers such as\n"),s("a",{attrs:{href:"https://github.com/iqlusioninc/tmkms",target:"_blank",rel:"noopener noreferrer"}},[e._v("tkkms"),s("OutboundLink")],1),e._v(". Such remote signers allow for signing\nof important Tendermint messages using\n"),s("a",{attrs:{href:"https://en.wikipedia.org/wiki/Hardware_security_module",target:"_blank",rel:"noopener noreferrer"}},[e._v("HSMs"),s("OutboundLink")],1),e._v(", providing\nadditional security.")]),e._v(" "),s("p",[e._v("When executed, "),s("code",[e._v("tm-signer-harness")]),e._v(":")]),e._v(" "),s("ol",[s("li",[e._v("Runs a listener (either TCP or Unix sockets).")]),e._v(" "),s("li",[e._v("Waits for a connection from the remote signer.")]),e._v(" "),s("li",[e._v("Upon connection from the remote signer, executes a number of automated tests\nto ensure compatibility.")]),e._v(" "),s("li",[e._v("Upon successful validation, the harness process exits with a 0 exit code.\nUpon validation failure, it exits with a particular exit code related to the\nerror.")])]),e._v(" "),s("h2",{attrs:{id:"prerequisites"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#prerequisites"}},[e._v("#")]),e._v(" Prerequisites")]),e._v(" "),s("p",[e._v("Requires the same prerequisites as for building\n"),s("a",{attrs:{href:"https://github.com/tendermint/tendermint",target:"_blank",rel:"noopener noreferrer"}},[e._v("Tendermint"),s("OutboundLink")],1),e._v(".")]),e._v(" "),s("h2",{attrs:{id:"building"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#building"}},[e._v("#")]),e._v(" Building")]),e._v(" "),s("p",[e._v("From the "),s("code",[e._v("tools/tm-signer-harness")]),e._v(" directory in your Tendermint source\nrepository, simply run:")]),e._v(" "),s("tm-code-block",{staticClass:"codeblock",attrs:{language:"bash",base64:"bWFrZQoKIyBUbyBoYXZlIGdsb2JhbCBhY2Nlc3MgdG8gdGhpcyBleGVjdXRhYmxlCm1ha2UgaW5zdGFsbAo="}}),e._v(" "),s("h2",{attrs:{id:"docker-image"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#docker-image"}},[e._v("#")]),e._v(" Docker Image")]),e._v(" "),s("p",[e._v("To build a Docker image containing the "),s("code",[e._v("tm-signer-harness")]),e._v(", also from the\n"),s("code",[e._v("tools/tm-signer-harness")]),e._v(" directory of your Tendermint source repo, simply run:")]),e._v(" "),s("tm-code-block",{staticClass:"codeblock",attrs:{language:"bash",base64:"bWFrZSBkb2NrZXItaW1hZ2UK"}}),e._v(" "),s("h2",{attrs:{id:"running-against-kms"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#running-against-kms"}},[e._v("#")]),e._v(" Running against KMS")]),e._v(" "),s("p",[e._v("As an example of how to use "),s("code",[e._v("tm-signer-harness")]),e._v(", the following instructions show\nyou how to execute its tests against "),s("a",{attrs:{href:"https://github.com/iqlusioninc/tmkms",target:"_blank",rel:"noopener noreferrer"}},[e._v("tkkms"),s("OutboundLink")],1),e._v(".\nFor this example, we will make use of the "),s("strong",[e._v("software signing module in KMS")]),e._v(", as\nthe hardware signing module requires a physical\n"),s("a",{attrs:{href:"https://www.yubico.com/products/yubihsm/",target:"_blank",rel:"noopener noreferrer"}},[e._v("YubiHSM"),s("OutboundLink")],1),e._v(" device.")]),e._v(" "),s("h3",{attrs:{id:"step-1-install-kms-on-your-local-machine"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#step-1-install-kms-on-your-local-machine"}},[e._v("#")]),e._v(" Step 1: Install KMS on your local machine")]),e._v(" "),s("p",[e._v("See the "),s("a",{attrs:{href:"https://github.com/iqlusioninc/tmkms",target:"_blank",rel:"noopener noreferrer"}},[e._v("tkkms repo"),s("OutboundLink")],1),e._v(" for details on how to set\nKMS up on your local machine.")]),e._v(" "),s("p",[e._v("If you have "),s("a",{attrs:{href:"https://www.rust-lang.org/",target:"_blank",rel:"noopener noreferrer"}},[e._v("Rust"),s("OutboundLink")],1),e._v(" installed on your local machine,\nyou can simply install KMS by:")]),e._v(" "),s("tm-code-block",{staticClass:"codeblock",attrs:{language:"bash",base64:"Y2FyZ28gaW5zdGFsbCB0bWttcwo="}}),e._v(" "),s("h3",{attrs:{id:"step-2-make-keys-for-kms"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#step-2-make-keys-for-kms"}},[e._v("#")]),e._v(" Step 2: Make keys for KMS")]),e._v(" "),s("p",[e._v("The KMS software signing module needs a key with which to sign messages. In our\nexample, we will simply export a signing key from our local Tendermint instance.")]),e._v(" "),s("tm-code-block",{staticClass:"codeblock",attrs:{language:"bash",base64:"IyBXaWxsIGdlbmVyYXRlIGFsbCBuZWNlc3NhcnkgVGVuZGVybWludCBjb25maWd1cmF0aW9uIGZpbGVzLCBpbmNsdWRpbmc6CiMgLSB+Ly50ZW5kZXJtaW50L2NvbmZpZy9wcml2X3ZhbGlkYXRvcl9rZXkuanNvbgojIC0gfi8udGVuZGVybWludC9kYXRhL3ByaXZfdmFsaWRhdG9yX3N0YXRlLmpzb24KdGVuZGVybWludCBpbml0IHZhbGlkYXRvcgoKIyBFeHRyYWN0IHRoZSBzaWduaW5nIGtleSBmcm9tIG91ciBsb2NhbCBUZW5kZXJtaW50IGluc3RhbmNlCnRtLXNpZ25lci1oYXJuZXNzIGV4dHJhY3Rfa2V5IFwgICAgICAjIFVzZSB0aGUgJnF1b3Q7ZXh0cmFjdF9rZXkmcXVvdDsgY29tbWFuZAogICAgLXRtaG9tZSB+Ly50ZW5kZXJtaW50IFwgICAgICAgICAgIyBXaGVyZSB0byBmaW5kIHRoZSBUZW5kZXJtaW50IGhvbWUgZGlyZWN0b3J5CiAgICAtb3V0cHV0IC4vc2lnbmluZy5rZXkgICAgICAgICAgICAjIFdoZXJlIHRvIHdyaXRlIHRoZSBrZXkK"}}),e._v(" "),s("p",[e._v("Also, because we want KMS to connect to "),s("code",[e._v("tm-signer-harness")]),e._v(", we will need to\nprovide a secret connection key from KMS' side:")]),e._v(" "),s("tm-code-block",{staticClass:"codeblock",attrs:{language:"bash",base64:"dG1rbXMga2V5Z2VuIHNlY3JldF9jb25uZWN0aW9uLmtleQo="}}),e._v(" "),s("h3",{attrs:{id:"step-3-configure-and-run-kms"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#step-3-configure-and-run-kms"}},[e._v("#")]),e._v(" Step 3: Configure and run KMS")]),e._v(" "),s("p",[e._v("KMS needs some configuration to tell it to use the softer signing module as well\nas the "),s("code",[e._v("signing.key")]),e._v(" file we just generated. Save the following to a file called\n"),s("code",[e._v("tmkms.toml")]),e._v(":")]),e._v(" "),s("tm-code-block",{staticClass:"codeblock",attrs:{language:"toml",base64:"W1t2YWxpZGF0b3JdXQphZGRyID0gJnF1b3Q7dGNwOi8vMTI3LjAuMC4xOjYxMjE5JnF1b3Q7ICAgICAgICAgIyBUaGlzIGlzIHdoZXJlIHdlIHdpbGwgZmluZCB0bS1zaWduZXItaGFybmVzcy4KY2hhaW5faWQgPSAmcXVvdDt0ZXN0LWNoYWluLTBYd1A1RSZxdW90OyAgICAgICAgICMgVGhlIFRlbmRlcm1pbnQgY2hhaW4gSUQgZm9yIHdoaWNoIEtNUyB3aWxsIGJlIHNpZ25pbmcgKGZvdW5kIGluIH4vLnRlbmRlcm1pbnQvY29uZmlnL2dlbmVzaXMuanNvbikuCnJlY29ubmVjdCA9IHRydWUgICAgICAgICAgICAgICAgICAgICAgICMgdHJ1ZSBpcyB0aGUgZGVmYXVsdApzZWNyZXRfa2V5ID0gJnF1b3Q7Li9zZWNyZXRfY29ubmVjdGlvbi5rZXkmcXVvdDsgIyBXaGVyZSB0byBmaW5kIG91ciBzZWNyZXQgY29ubmVjdGlvbiBrZXkuCgpbW3Byb3ZpZGVycy5zb2Z0c2lnbl1dCmlkID0gJnF1b3Q7dGVzdC1jaGFpbi0wWHdQNUUmcXVvdDsgICAgICAgICAgICAgICAjIFRoZSBUZW5kZXJtaW50IGNoYWluIElEIGZvciB3aGljaCBLTVMgd2lsbCBiZSBzaWduaW5nIChzYW1lIGFzIHZhbGlkYXRvci5jaGFpbl9pZCBhYm92ZSkuCnBhdGggPSAmcXVvdDsuL3NpZ25pbmcua2V5JnF1b3Q7ICAgICAgICAgICAgICAgICAjIFRoZSBzaWduaW5nIGtleSB3ZSBleHRyYWN0ZWQgZWFybGllci4K"}}),e._v(" "),s("p",[e._v("Then run KMS with this configuration:")]),e._v(" "),s("tm-code-block",{staticClass:"codeblock",attrs:{language:"bash",base64:"dG1rbXMgc3RhcnQgLWMgdG1rbXMudG9tbAo="}}),e._v(" "),s("p",[e._v("This will start KMS, which will repeatedly try to connect to\n"),s("code",[e._v("tcp://127.0.0.1:61219")]),e._v(" until it is successful.")]),e._v(" "),s("h3",{attrs:{id:"step-4-run-tm-signer-harness"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#step-4-run-tm-signer-harness"}},[e._v("#")]),e._v(" Step 4: Run tm-signer-harness")]),e._v(" "),s("p",[e._v("Now we get to run the signer test harness:")]),e._v(" "),s("tm-code-block",{staticClass:"codeblock",attrs:{language:"bash",base64:"dG0tc2lnbmVyLWhhcm5lc3MgcnVuIFwgICAgICAgICAgICAgIyBUaGUgJnF1b3Q7cnVuJnF1b3Q7IGNvbW1hbmQgZXhlY3V0ZXMgdGhlIHRlc3RzCiAgICAtYWRkciB0Y3A6Ly8xMjcuMC4wLjE6NjEyMTkgXCAgICMgVGhlIGFkZHJlc3Mgd2UgcHJvbWlzZWQgS01TIGVhcmxpZXIKICAgIC10bWhvbWUgfi8udGVuZGVybWludCAgICAgICAgICAgIyBXaGVyZSB0byBmaW5kIG91ciBUZW5kZXJtaW50IGNvbmZpZ3VyYXRpb24vZGF0YSBmaWxlcy4K"}}),e._v(" "),s("p",[e._v("If the current version of Tendermint and KMS are compatible, "),s("code",[e._v("tm-signer-harness")]),e._v("\nshould now exit with a 0 exit code. If they are somehow not compatible, it\nshould exit with a meaningful non-zero exit code (see the exit codes below).")]),e._v(" "),s("h3",{attrs:{id:"step-5-shut-down-kms"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#step-5-shut-down-kms"}},[e._v("#")]),e._v(" Step 5: Shut down KMS")]),e._v(" "),s("p",[e._v("Simply hit Ctrl+Break on your KMS instance (or use the "),s("code",[e._v("kill")]),e._v(" command in Linux)\nto terminate it gracefully.")]),e._v(" "),s("h2",{attrs:{id:"exit-code-meanings"}},[s("a",{staticClass:"header-anchor",attrs:{href:"#exit-code-meanings"}},[e._v("#")]),e._v(" Exit Code Meanings")]),e._v(" "),s("p",[e._v("The following list shows the various exit codes from "),s("code",[e._v("tm-signer-harness")]),e._v(" and\ntheir meanings:")]),e._v(" "),s("table",[s("thead",[s("tr",[s("th",[e._v("Exit Code")]),e._v(" "),s("th",[e._v("Description")])])]),e._v(" "),s("tbody",[s("tr",[s("td",[e._v("0")]),e._v(" "),s("td",[e._v("Success!")])]),e._v(" "),s("tr",[s("td",[e._v("1")]),e._v(" "),s("td",[e._v("Invalid command line parameters supplied to "),s("code",[e._v("tm-signer-harness")])])]),e._v(" "),s("tr",[s("td",[e._v("2")]),e._v(" "),s("td",[e._v("Maximum number of accept retries reached (the "),s("code",[e._v("-accept-retries")]),e._v(" parameter)")])]),e._v(" "),s("tr",[s("td",[e._v("3")]),e._v(" "),s("td",[e._v("Failed to load "),s("code",[e._v("${TMHOME}/config/genesis.json")])])]),e._v(" "),s("tr",[s("td",[e._v("4")]),e._v(" "),s("td",[e._v("Failed to create listener specified by "),s("code",[e._v("-addr")]),e._v(" parameter")])]),e._v(" "),s("tr",[s("td",[e._v("5")]),e._v(" "),s("td",[e._v("Failed to start listener")])]),e._v(" "),s("tr",[s("td",[e._v("6")]),e._v(" "),s("td",[e._v("Interrupted by "),s("code",[e._v("SIGINT")]),e._v(" (e.g. when hitting Ctrl+Break or Ctrl+C)")])]),e._v(" "),s("tr",[s("td",[e._v("7")]),e._v(" "),s("td",[e._v("Other unknown error")])]),e._v(" "),s("tr",[s("td",[e._v("8")]),e._v(" "),s("td",[e._v("Test 1 failed: public key mismatch")])]),e._v(" "),s("tr",[s("td",[e._v("9")]),e._v(" "),s("td",[e._v("Test 2 failed: signing of proposals failed")])]),e._v(" "),s("tr",[s("td",[e._v("10")]),e._v(" "),s("td",[e._v("Test 3 failed: signing of votes failed")])])])])],1)}),[],!1,null,null,null);t.default=n.exports}}]);