/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package endorser

import (
	"fmt"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/ledger/rwset/kvrwset"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/common/util"
)

// cleanupExpiredDependencies periodically removes expired dependency entries
func (e *Endorser) cleanupExpiredDependencies() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			now := time.Now()
			removedCount := 0
			entriesToRemove := make([]string, 0)

			e.VariableMapLock.RLock()
			for key, info := range e.VariableMap {
				if now.After(info.ExpiryTime) {
					entriesToRemove = append(entriesToRemove, key)
					removedCount++
					logger.Debugf("Marked expired dependency for variable %s", key)
				}
			}
			e.VariableMapLock.RUnlock()

			if len(entriesToRemove) > 0 {
				e.VariableMapLock.Lock()
				for _, key := range entriesToRemove {
					delete(e.VariableMap, key)
				}

				if e.Metrics.ExpiredDependenciesRemoved != nil {
					e.Metrics.ExpiredDependenciesRemoved.Add(float64(removedCount))
				}

				if e.Metrics.DependencyMapSize != nil {
					e.Metrics.DependencyMapSize.Set(float64(len(e.VariableMap)))
				}

				logger.Infof("Dependency cleanup completed: %d expired entries removed, current map size: %d",
					removedCount, len(e.VariableMap))

				e.VariableMapLock.Unlock()
			}
		}
	}
}

// processTransactions handles transaction processing for the leader endorser
func (e *Endorser) processTransactions() {
	for {
		select {
		case <-e.stopChan:
			return
		case tx := <-e.TxChannel:
			processedTx, err := e.processTransaction(tx)
			if err != nil {
				logger.Errorf("Error processing transaction: %v", err)
				continue
			}
			e.ResponseChannel <- processedTx
		}
	}
}

// extractDependencyInfo extracts dependency information from a transaction
func (e *Endorser) extractDependencyInfo(tx *pb.ProposalResponse) (*DependencyInfo, error) {
	chaincodeAction := &pb.ChaincodeAction{}
	if err := proto.Unmarshal(tx.Payload, chaincodeAction); err != nil {
		return nil, err
	}

	rwSet := &kvrwset.KVRWSet{}
	if err := proto.Unmarshal(chaincodeAction.Results, rwSet); err != nil {
		return nil, err
	}

	depInfo := &DependencyInfo{
		HasDependency: false,
	}

	for _, read := range rwSet.Reads {
		e.VariableMapLock.RLock()
		if info, exists := e.VariableMap[read.Key]; exists {
			depInfo.HasDependency = true
			depInfo.DependentTxID = info.DependentTxID
			depInfo.Value = info.Value
		}
		e.VariableMapLock.RUnlock()
	}

	return depInfo, nil
}

// processTransaction processes a single transaction in the leader endorser
func (e *Endorser) processTransaction(tx *pb.ProposalResponse) (*pb.ProposalResponse, error) {
	txID := util.GenerateUUID()
	depInfo, err := e.extractDependencyInfo(tx)
	if err != nil {
		return nil, err
	}

	e.VariableMapLock.Lock()
	e.VariableMap[txID] = TransactionDependencyInfo{
		Value:         depInfo.Value,
		DependentTxID: depInfo.DependentTxID,
		ExpiryTime:    time.Now().Add(e.EndorsementExpiryDuration),
		HasDependency: depInfo.HasDependency,
	}
	e.VariableMapLock.Unlock()

	tx.Response.Message = fmt.Sprintf("DependencyInfo:HasDependency=%v,DependentTxID=%s,ExpiryTime=%d",
		depInfo.HasDependency, depInfo.DependentTxID, time.Now().Add(e.EndorsementExpiryDuration).Unix())

	return tx, nil
}
	