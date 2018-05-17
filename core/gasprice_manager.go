// Copyright 2018 Vixeka Software Solutions, Inc.
// This file is part of the Theos library.
//
// The Theos library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Theos library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Theos library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

type GasPriceManager struct {
	config         GasPriceManagerConfig
	databaseReader DatabaseReader
}

// GasPriceManagerConfig are the configuration parameters of the gas price manager.
type GasPriceManagerConfig struct {
	FixedPrice         uint64 // The fixed gas price for transactions with fixed price.
	FixedPriceGasLimit uint64 // The gas limit for transactions with fixed price (the
	// gas price of transactions below this gas limit are set to fixed price).
	FixedPriceTxCountPerContractLimit uint64 // The number of allowed transactions per contract with
	// fixed price per day.
}

// DefaultGasPriceManagerConfig contains the default configurations for the transaction
// pool.
var DefaultGasPriceManagerConfig = GasPriceManagerConfig{
	FixedPrice:                        0,
	FixedPriceGasLimit:                100000,
	FixedPriceTxCountPerContractLimit: 10000,
}

// Calculates the expected fixed price based on the number of transactions in the database.
func SetExpectedGasPrice(databaseReader DatabaseReader, tx *types.Transaction) {
	gasPriceManager := NewGasPriceManager(databaseReader)
	actualGasPrice, _ := gasPriceManager.GetActualGasPrice(tx.To(), tx.Gas(), tx.GasPrice())
	tx.SetExpectedGasPrice(actualGasPrice)
}

func NewGasPriceManager(databaseReader DatabaseReader) *GasPriceManager {

	config := DefaultGasPriceManagerConfig

	// Create the gas price manager with its initial settings
	gpm := &GasPriceManager{
		config:         config,
		databaseReader: databaseReader,
	}

	return gpm
}

func (gpm *GasPriceManager) GetActualGasPrice(to *common.Address, gasUsed uint64, gasPrice *big.Int) (actualGasPrice *big.Int, isFixedPriceApplied bool) {

	actualGasPrice = gasPrice
	isFixedPriceApplied = false
	if gpm.isFixedPriceShouldBeApplied(to, gasUsed) {
		actualGasPrice = big.NewInt(int64(gpm.config.FixedPrice))
		isFixedPriceApplied = true
	}

	return actualGasPrice, isFixedPriceApplied
}

func (gpm *GasPriceManager) isFixedPriceShouldBeApplied(to *common.Address, txGasUsed uint64) bool {

	// If the transaction gas is over the fixed price limit, do not continue. As the fixed price
	// cannot be applied.
	if txGasUsed > gpm.config.FixedPriceGasLimit {
		return false
	}

	blockHash := GetHeadBlockHash(gpm.databaseReader)
	if blockHash == (common.Hash{}) {
		// Corrupt or empty database, init from scratch
		log.Warn("Empty database, gas considered as below the limit to apply the fixed price")
		return true
	}

	blockNumber := GetBlockNumber(gpm.databaseReader, blockHash)
	if blockNumber == missingNumber {
		// Corrupt or empty database
		log.Warn("Empty database, gas considered as below the limit to apply the fixed price")
		return true
	}

	fixedPriceTxCountPerContract := uint64(0)

	yearOfNow, monthOfNow, dayOfNow := time.Now().UTC().Date()
	dayBeginningOfNowTime := big.NewInt(time.Date(yearOfNow, monthOfNow, dayOfNow, 0, 0, 0, 0, time.UTC).Unix())

	for {
		if blockHash == (common.Hash{}) {
			break
		}

		block := GetBlock(gpm.databaseReader, blockHash, blockNumber)

		if block == nil {
			// Corrupt database
			log.Warn("Could not return block data, gas considered as over the limit to apply the fixed price")
			return false
		}

		if block.Time().CmpAbs(dayBeginningOfNowTime) < 0 {
			// Leaves the loop as this means we already processed all required blocks.
			break
		}

		if block.Transactions() != nil {
			for _, transaction := range block.Transactions() {
				// Now we detect whether the Fixed Price was applied to transaction just checking that
				// the trasaction used gas is below the GasLimit.
				// TODO: In future implement the solution that should mark the transaction where
				// FixedPrice was applied.
				if transaction.Gas() <= gpm.config.FixedPriceGasLimit && *transaction.To() == *to {
					fixedPriceTxCountPerContract++
				}
			}
		}

		if fixedPriceTxCountPerContract > gpm.config.FixedPriceTxCountPerContractLimit {
			break
		}

		blockHash = block.ParentHash()
		blockNumber = blockNumber - 1
	}

	// If the number of transactions below the limit.
	return fixedPriceTxCountPerContract <= gpm.config.FixedPriceTxCountPerContractLimit
}
