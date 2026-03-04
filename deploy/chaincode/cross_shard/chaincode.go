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
	// Each arg from index 2 onward is a secondary shard to invoke
	for i := 2; i < len(args); i++ {
		if args[i] == "" {
			continue
		}
		secondaryShard := args[i]
		channelID := stub.GetChannelID()

		// Invoke secondary shard
		response := stub.InvokeChaincode(secondaryShard, [][]byte{[]byte("invoke"), []byte(fmt.Sprintf("cross_%d_%s", i-1, primaryKey)), []byte(value)}, channelID)

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
