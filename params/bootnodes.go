// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package params

// MainnetBootnodes are the enode URLs of the P2P bootstrap nodes running on
// the main Ethereum network.
var MainnetBootnodes = []string{
    // Oggchain Bootnodes
    "enode://051db10a92c711067e1c96ecdaabfc65d8481435b430609826d206884cf8c5abb1f4c8b3c51b6df9d7c97ec4662b4d4249e0a065a36a7e451d9906b2ce5e4d6b@85.190.254.195:30666",
    "enode://3be4a6123d56eca56a7f66f99a99bbf2a8c47b014618d9e37ce74181a1ab3e66d2c26e517821141e6c62472e7c784e8df4032d80bf7901011a4c2640b51af216@85.190.254.196:30667",
    "enode://5ca09c49ae55097b1c91898dae77ca003e88458ac6ce5cae5722dd2a41584c9a915896b3ca18f6fcdd470720668b1648e074ccbae7e18a89cdd0c4a5cdbeb266@81.17.99.108:30668",
}

// TestnetBootnodes are the enode URLs of the P2P bootstrap nodes running on the
// RoGem test network.
var TestnetBootnodes = []string{

}

// RinkebyBootnodes are the enode URLs of the P2P bootstrap nodes running on the
// Rinkeby test network.
var RinkebyBootnodes = []string{

}

// DiscoveryV5Bootnodes are the enode URLs of the P2P bootstrap nodes for the
// experimental RLPx v5 topic-discovery network.
var DiscoveryV5Bootnodes = []string{

}
