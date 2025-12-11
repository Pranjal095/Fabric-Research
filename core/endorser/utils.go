/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package endorser

import (
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/ledger/rwset/kvrwset"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/core/common/ccprovider"
	"github.com/hyperledger/fabric/core/ledger"
)

// decorateLogger adds transaction context to the logger
func decorateLogger(logger *flogging.FabricLogger, txParams *ccprovider.TransactionParams) *flogging.FabricLogger {
	return logger.With("channel", txParams.ChannelID, "txID", shorttxid(txParams.TxID))
}

// shorttxid shortens transaction IDs for logging
func shorttxid(txid string) string {
	if len(txid) < 8 {
		return txid
	}
	return txid[0:8]
}

// acquireTxSimulator determines whether a transaction simulator should be obtained
func acquireTxSimulator(chainID string, chaincodeName string) bool {
	if chainID == "" {
		return false
	}

	// Don't get a simulator for query and config system chaincodes
	// These don't need the simulator and its read lock results in deadlocks
	switch chaincodeName {
	case "qscc", "cscc":
		return false
	default:
		return true
	}
}

// CreateCCEventBytes marshals a chaincode event to bytes
func CreateCCEventBytes(ccevent *pb.ChaincodeEvent) ([]byte, error) {
	if ccevent == nil {
		return nil, nil
	}
	return proto.Marshal(ccevent)
}

// extractTransactionDependencies identifies variables that the transaction operates on
func (e *Endorser) extractTransactionDependencies(simResult *ledger.TxSimulationResults) (map[string][]byte, error) {
	dependencies := make(map[string][]byte)

	// Extract variables from public state
	if simResult.PubSimulationResults != nil {
		for _, nsRWSet := range simResult.PubSimulationResults.NsRwset {
			namespace := nsRWSet.Namespace

			if e.Support.IsSysCC(namespace) {
				continue
			}

			kvRWSet := &kvrwset.KVRWSet{}
			if err := proto.Unmarshal(nsRWSet.Rwset, kvRWSet); err != nil {
				logger.Warningf("Failed to unmarshal rwset for namespace %s: %s", namespace, err)
				continue
			}

			// Extract write dependencies
			for _, write := range kvRWSet.Writes {
				key := namespace + ":" + string(write.Key)
				dependencies[key] = write.Value
				logger.Debugf("Transaction write dependency identified: %s", key)
			}

			// Extract read dependencies
			for _, read := range kvRWSet.Reads {
				key := namespace + ":" + string(read.Key)
				if _, exists := dependencies[key]; !exists {
					if read.Version != nil {
						versionBytes := []byte(fmt.Sprintf("%d-%d", read.Version.BlockNum, read.Version.TxNum))
						dependencies[key] = versionBytes
					} else {
						dependencies[key] = []byte{}
					}
					logger.Debugf("Transaction read dependency identified: %s", key)
				}
			}
		}
	}

	// Extract variables from private data
	if simResult.PvtSimulationResults != nil {
		for _, pvtRWSet := range simResult.PvtSimulationResults.NsPvtRwset {
			namespace := pvtRWSet.Namespace

			if e.Support.IsSysCC(namespace) {
				continue
			}

			for _, collection := range pvtRWSet.CollectionPvtRwset {
				collectionName := collection.CollectionName

				collKVRWSet := &kvrwset.KVRWSet{}
				if err := proto.Unmarshal(collection.Rwset, collKVRWSet); err != nil {
					logger.Warningf("Failed to unmarshal collection rwset for namespace %s, collection %s: %s",
						namespace, collectionName, err)
					continue
				}

				// Extract private write dependencies
				for _, write := range collKVRWSet.Writes {
					key := namespace + ":" + collectionName + ":" + string(write.Key)
					dependencies[key] = write.Value
					logger.Debugf("Private data write dependency identified: %s", key)
				}

				// Extract private read dependencies
				for _, read := range collKVRWSet.Reads {
					key := namespace + ":" + collectionName + ":" + string(read.Key)
					if _, exists := dependencies[key]; !exists {
						if read.Version != nil {
							versionBytes := []byte(fmt.Sprintf("%d-%d", read.Version.BlockNum, read.Version.TxNum))
							dependencies[key] = versionBytes
						} else {
							dependencies[key] = []byte{}
						}
						logger.Debugf("Private data read dependency identified: %s", key)
					}
				}
			}
		}
	}

	return dependencies, nil
}
