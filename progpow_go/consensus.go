// Copyright 2017 The go-ethereum Authors
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

package progpow_go

import (
	"bytes"
	"errors"
	"math/big"
	"runtime"

	"github.com/sero-cash/go-czero-import/seroparam"

	"github.com/sero-cash/mine-pool/ethash"
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	errInvalidDifficulty = errors.New("non-positive difficulty")
	errInvalidMixDigest  = errors.New("invalid mix digest")
	errInvalidPoW        = errors.New("invalid proof-of-work")
)

// VerifySeal implements consensus.Engine, checking whether the given block satisfies
// the PoW difficulty requirements.
func (ethash *Ethash) Verify(block ethash.Block) bool {
	// Ensure that we have a valid difficulty for the block
	if block.Difficulty().Sign() <= 0 {
		return false
	}
	// Recompute the digest and PoW value and verify against the header
	number := block.NumberU64()

	cache := ethash.cache(number)
	size := datasetSize(number)

	var digest []byte
	var result []byte
	if number >= seroparam.SIP3 {
		dataset := ethash.dataset_async(number)
		if dataset.generated() {
			digest, result = progpowFull(dataset.dataset, block.HashNoNonce().Bytes(), block.Nonce(), number)
		} else {
			digest, result = progpowLightWithoutCDag(size, cache.cache, block.HashNoNonce().Bytes(), block.Nonce(), number)
		}
	} else {
		digest, result = hashimotoLight(size, cache.cache, block.HashNoNonce().Bytes(), block.Nonce(), number)
	}
	// Caches are unmapped in a finalizer. Ensure that the cache stays live
	// until after the call to hashimotoLight so it's not unmapped while being used.
	runtime.KeepAlive(cache)

	md := block.MixDigest()
	if !bytes.Equal(md[:], digest) {
		return false
	}
	target := new(big.Int).Div(maxUint256, block.Difficulty())
	if new(big.Int).SetBytes(result).Cmp(target) > 0 {
		return false
	}
	return true
}
