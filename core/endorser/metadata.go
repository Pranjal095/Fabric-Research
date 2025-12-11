/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package endorser

import (
	"github.com/hyperledger/fabric-protos-go/common"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/core/ledger"
	"github.com/hyperledger/fabric/protoutil"
)

type metadataPolicies struct {
	sbePolicies    map[string][]*common.SignaturePolicyEnvelope
	policyRequired map[string]map[string]bool
}

func parseWritesetMetadata(metadata ledger.WritesetMetadata) (*metadataPolicies, error) {
	mp := &metadataPolicies{
		sbePolicies:    map[string][]*common.SignaturePolicyEnvelope{},
		policyRequired: map[string]map[string]bool{},
	}
	for ns, cmap := range metadata {
		mp.policyRequired[ns] = map[string]bool{"": false}
		for coll, kmap := range cmap {
			for _, stateMetadata := range kmap {
				if policyBytes, sbeExists := stateMetadata[pb.MetaDataKeys_VALIDATION_PARAMETER.String()]; sbeExists {
					policy, err := protoutil.UnmarshalSignaturePolicy(policyBytes)
					if err != nil {
						return nil, err
					}
					mp.sbePolicies[ns] = append(mp.sbePolicies[ns], policy)
				} else {
					mp.policyRequired[ns][coll] = true
				}
			}
		}
	}

	return mp, nil
}

func (mp *metadataPolicies) add(ns string, coll string, required bool) {
	if entry, ok := mp.policyRequired[ns]; ok {
		entry[coll] = required
	} else {
		mp.policyRequired[ns] = map[string]bool{coll: required}
	}
}

func (mp *metadataPolicies) requireChaincodePolicy(ns string) bool {
	return mp.policyRequired[ns][""]
}
