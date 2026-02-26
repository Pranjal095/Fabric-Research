package main

import (
	"fmt"

	"github.com/hyperledger/fabric-chaincode-go/shim"
	pb "github.com/hyperledger/fabric-protos-go/peer"
)

type CrossShardChaincode struct{}

func (t *CrossShardChaincode) Init(stub shim.ChaincodeStubInterface) pb.Response {
	return shim.Success(nil)
}

func (t *CrossShardChaincode) Invoke(stub shim.ChaincodeStubInterface) pb.Response {
	fn, args := stub.GetFunctionAndParameters()

	if fn != "invoke" {
		return shim.Error("Invalid function name. Expecting 'invoke'")
	}
	if len(args) < 2 {
		return shim.Error("Incorrect arguments. Expecting primaryKey, value, [secondaryShard]")
	}

	primaryKey := args[0]
	value := args[1]

	// Write to primary shard
	err := stub.PutState(primaryKey, []byte(value))
	if err != nil {
		return shim.Error(err.Error())
	}

	// Cross-shard secondary invocation logic based on Caliper workload
	if len(args) >= 3 && args[2] != "" {
		secondaryShard := args[2]
		channelID := stub.GetChannelID()

		// Invoke secondary shard (it is exact same chaincode so we call invoke)
		response := stub.InvokeChaincode(secondaryShard, [][]byte{[]byte("invoke"), []byte("cross_" + primaryKey), []byte(value)}, channelID)

		if response.Status != shim.OK {
			return shim.Error(fmt.Sprintf("Failed to invoke cross-shard chaincode %s: %s", secondaryShard, response.Message))
		}
	}

	return shim.Success([]byte("Transaction recorded successfully"))
}

func main() {
	err := shim.Start(new(CrossShardChaincode))
	if err != nil {
		fmt.Printf("Error starting CrossShard chaincode: %s", err)
	}
}
