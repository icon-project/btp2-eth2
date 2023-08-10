/*
 * Copyright 2023 ICON Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package eth2

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestTransaction(t *testing.T) {
	ts := time.Now()
	txHash := common.HexToHash("0xfb9a68914db86ef3aef1465130692aab0f0c290058e2ae1339ac145f764dd73b")

	ltx := newTransaction(ts, txHash)
	bs, err := ltx.Bytes()
	assert.NoError(t, err)

	ltx2, err := newTransactionFromBytes(bs)
	assert.NoError(t, err)

	assert.Equal(t, ltx.TxHash, ltx2.TxHash)
	assert.True(t, ltx.TS.Equal(ltx2.TS))
	assert.True(t, ltx.ExpireTS.Equal(ltx2.ExpireTS))
}
