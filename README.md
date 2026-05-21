# 🪨 OGG Chain — Go Implementation

Official Go implementation of the Oggchain protocol.

Forked from [go-egem](https://github.com/TeamEGEM/go-egem) — extended with OggPoW, a custom ProgPoW implementation written in Go, and configured for the Oggchain mainchain.

---

## ⛏️ OggPoW — Custom ProgPoW Implementation

Oggchain runs **OggPoW** — a custom Go implementation of the ProgPoW algorithm.

ProgPoW (Programmatic Proof of Work) is designed to close the efficiency gap available to specialized ASICs. OggPoW is tuned specifically for Oggchain with:

- **Block time:** ~12 seconds
- **Algorithm:** OggPoW (ProgPoW, custom Go implementation)
- **ASIC resistant** — GPU focused from block one
- **Network ID:** 19870
- **RPC Port:** 18545
- **P2P Port:** 30666

> 🪨 GPU miners built this chain. ASIC farms not welcome.

---

## 🪨 Oggchain — Tokenomics

**Max Supply: 10,700,000,000 OGG — Hard cap. Carved forever.**

| Allocation | Amount | % |
|---|---|---|
| ⛏️ Block Rewards (mined over 15 years) | 10,000,000,000 OGG | 93.5% |
| 🪙 Premine | 700,000,000 OGG | 6.5% |

**Block reward split — every block, forever:**

| Recipient | % | Total over 15 years |
|---|---|---|
| ⛏️ GPU Miners | 45% | ~4,500,000,000 OGG |
| 🔒 Staking Pool | 40% | ~4,000,000,000 OGG |
| 🏛️ Tribe Pool (governance) | 7% | ~700,000,000 OGG |
| 🔧 Maintenance | 8% | ~800,000,000 OGG |

Block rewards decay smoothly using exponential decay — no halvings, no sudden cliffs.

```
Block Reward = 568.39 × 0.99999995157570044 ^ BlockNumber
```

---

## 📖 Documentation

Full documentation, tokenomics, staking guide, CLI guide, and mining instructions:

**[📖 The Book of OGG — GitBook](https://oggcoin.gitbook.io/the-book-of-ogg)**

---

## 🚀 Quick Start — Run a Node

The easiest way to run an OGG node is via Docker.

**Full Docker setup guide:**
→ See [Ogg-Node repository](https://github.com/Oggcoin/Ogg-Node)

---

## 🔨 Building from Source

### Prerequisites

- Go 1.10 or later
- C compiler

Install dependencies and build:

```bash
make egem
```

Or build the full suite of utilities:

```bash
make all
```

---

## 🖥️ Executables

The go-egem project comes with several wrappers/executables found in the `cmd` directory.

| Command | Description |
|:---:|---|
| **`egem`** | Main OGG CLI client. Entry point into the Oggchain. Runs as full node, archive node, or light node. Exposes JSON RPC over HTTP, WebSocket, and IPC. |
| `abigen` | Source code generator to convert contract definitions into type-safe Go packages. |
| `bootnode` | Lightweight bootstrap node for peer discovery in the network. |
| `evm` | Developer utility for running EVM bytecode snippets in an isolated environment. |
| `rlpdump` | Utility to convert binary RLP dumps to human-readable format. |
| `puppeth` | CLI wizard for creating a new Ethereum-compatible network. |

---

## ⚡ Running the Node

### Full node on Oggchain mainchain

```bash
$ egem console
```

This starts the node in fast sync mode and opens the interactive JavaScript console.

Attach to an already running node:

```bash
$ egem attach
```

### Configuration

Pass a configuration file:

```bash
$ egem --config /path/to/your_config.toml
```

Export your existing configuration:

```bash
$ egem --your-favourite-flags dumpconfig
```

### Oggchain connection flags

```bash
$ egem --networkid 19870 --rpc --rpcport 18545 --rpcaddr 0.0.0.0
```

---

## 🔌 JSON RPC

Connect to a running OGG node via HTTP, WebSocket, or IPC and use [JSON-RPC](http://www.jsonrpc.org/specification).

**Default RPC endpoint:**
```
http://127.0.0.1:18545
```

**Test RPC is alive:**
```bash
curl -s http://127.0.0.1:18545 \
-H "Content-Type: application/json" \
--data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

> ⚠️ Be careful exposing RPC publicly. Always restrict access with `--rpcaddr` and firewall rules.

---

## 🔧 Private Network Setup

### Define genesis state

Create a `genesis.json` file:

```json
{
  "config": {
    "chainId": 19870,
    "homesteadBlock": 0,
    "eip155Block": 0,
    "eip158Block": 0
  },
  "alloc": {},
  "coinbase": "0x0000000000000000000000000000000000000000",
  "difficulty": "0x20000",
  "extraData": "",
  "gasLimit": "0x2fefd8",
  "nonce": "0x0000000000000042",
  "mixhash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "timestamp": "0x00"
}
```

Initialize all nodes with genesis:

```bash
$ egem init path/to/genesis.json
```

### Start bootnode

```bash
$ bootnode --genkey=boot.key
$ bootnode --nodekey=boot.key
```

### Start member nodes

```bash
$ egem --datadir=path/to/data --bootnodes=<bootnode-enode-url>
```

---

## 🤝 Contribution

Contributions are welcome. Fork, fix, commit, and open a pull request.

Please follow these guidelines:

- Code must follow official Go [formatting](https://golang.org/doc/effective_go.html#formatting) guidelines (`gofmt`)
- Code must be documented following Go [commentary](https://golang.org/doc/effective_go.html#commentary) guidelines
- Pull requests must be based on and opened against the `master` branch
- Commit messages should be prefixed with the package(s) they modify
  - E.g. `eth, rpc: make trace configs optional`

---

## 🔗 Links

| | |
|---|---|
| 🌐 Website | https://oggcoin.org |
| 📖 Docs | https://oggcoin.gitbook.io/the-book-of-ogg |
| ⛏️ Mining Pool | https://pool.oggcoin.org |
| 💬 Telegram | https://t.me/proveyouogg |
| 🐦 Twitter/X | https://x.com/oggcave |
| 🎮 Discord | https://discord.gg/VrBQz7upZb |
| 💻 GitHub | https://github.com/Oggcoin |

---

## 📄 License

The go-egem library (all code outside of the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html),
included in this repository in the `COPYING.LESSER` file.

The go-egem binaries (all code inside of the `cmd` directory) are licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html),
included in this repository in the `COPYING` file.

---

🪨 **OGG Chain. GPU mined. Community governed. Carved in stone.**
