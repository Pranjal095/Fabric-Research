/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

/*
Transaction Dependency Tracking Enhancement for Hyperledger Fabric Endorser

This implementation adds transaction dependency tracking to the Fabric endorser component.
The primary features include:

1. Variable Tracking: Maintains a hashmap of variables (keys) that transactions operate on,
   along with their current values and the transaction that last modified them.

2. Dependency Detection: When a transaction accesses a variable that exists in the hashmap,
   the system marks it as dependent on the transaction that previously modified that variable.

3. Dependency Information in Responses: The endorser includes dependency information in the
   endorsement response, allowing clients to be aware of transaction dependencies.

4. Sharded Raft-based Resolution: Uses contract-based sharding with Raft consensus for
   scalable dependency management across multiple endorser nodes.

5. Metrics Collection: The implementation includes metrics to track dependency-related statistics.
*/

package endorser

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hyperledger/fabric-chaincode-go/shim"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric-protos-go/transientstore"
	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/core/chaincode/lifecycle"
	"github.com/hyperledger/fabric/core/common/ccprovider"
	"github.com/hyperledger/fabric/core/endorser/sharding"
	"github.com/hyperledger/fabric/core/ledger"
	"github.com/hyperledger/fabric/internal/pkg/identity"
	"github.com/hyperledger/fabric/msp"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var logger = flogging.MustGetLogger("endorser")

const (
	DefaultPrepareTimeout = 2000 * time.Millisecond
)

// TransactionDependencyInfo represents information about a transaction dependency
type TransactionDependencyInfo struct {
	Value         []byte    // The current value of the variable
	DependentTxID string    // ID of the transaction this depends on (if any)
	ExpiryTime    time.Time // When this endorsement expires
	HasDependency bool      // Whether this transaction has a dependency
}

// DependencyInfo represents the dependency information for a transaction
type DependencyInfo struct {
	Value         []byte
	DependentTxID string
	HasDependency bool
}

//go:generate counterfeiter -o fake/prvt_data_distributor.go --fake-name PrivateDataDistributor . PrivateDataDistributor

// PrivateDataDistributor distributes private data to authorized peers
type PrivateDataDistributor interface {
	DistributePrivateData(channel string, txID string, privateData *transientstore.TxPvtReadWriteSetWithConfigInfo, blkHt uint64) error
}

// Support contains functions that the endorser requires to execute its tasks
type Support interface {
	identity.SignerSerializer
	// GetTxSimulator returns the transaction simulator for the specified ledger
	// a client may obtain more than one such simulator; they are made unique
	// by way of the supplied txid
	GetTxSimulator(ledgername string, txid string) (ledger.TxSimulator, error)

	// GetHistoryQueryExecutor gives handle to a history query executor for the
	// specified ledger
	GetHistoryQueryExecutor(ledgername string) (ledger.HistoryQueryExecutor, error)

	// GetTransactionByID retrieves a transaction by id
	GetTransactionByID(chid, txID string) (*pb.ProcessedTransaction, error)

	// IsSysCC returns true if the name matches a system chaincode's
	// system chaincode names are system, chain wide
	IsSysCC(name string) bool

	// Execute - execute proposal, return original response of chaincode
	Execute(txParams *ccprovider.TransactionParams, name string, input *pb.ChaincodeInput) (*pb.Response, *pb.ChaincodeEvent, error)

	// ExecuteLegacyInit - executes a deployment proposal, return original response of chaincode
	ExecuteLegacyInit(txParams *ccprovider.TransactionParams, name, version string, spec *pb.ChaincodeInput) (*pb.Response, *pb.ChaincodeEvent, error)

	// ChaincodeEndorsementInfo returns the information from lifecycle required to endorse the chaincode.
	ChaincodeEndorsementInfo(channelID, chaincodeID string, txsim ledger.QueryExecutor) (*lifecycle.ChaincodeEndorsementInfo, error)

	// CheckACL checks the ACL for the resource for the channel using the
	// SignedProposal from which an id can be extracted for testing against a policy
	CheckACL(channelID string, signedProp *pb.SignedProposal) error

	// EndorseWithPlugin endorses the response with a plugin
	EndorseWithPlugin(pluginName, channnelID string, prpBytes []byte, signedProposal *pb.SignedProposal) (*pb.Endorsement, []byte, error)

	// GetLedgerHeight returns ledger height for given channelID
	GetLedgerHeight(channelID string) (uint64, error)

	// GetDeployedCCInfoProvider returns ledger.DeployedChaincodeInfoProvider
	GetDeployedCCInfoProvider() ledger.DeployedChaincodeInfoProvider
}

//go:generate counterfeiter -o fake/channel_fetcher.go --fake-name ChannelFetcher . ChannelFetcher

// ChannelFetcher fetches the channel context for a given channel ID
type ChannelFetcher interface {
	Channel(channelID string) *Channel
}

// Channel represents a channel context
type Channel struct {
	IdentityDeserializer msp.IdentityDeserializer
}

// EndorserRole represents the role of an endorser in the network
type EndorserRole int

const (
	NormalEndorser EndorserRole = iota
	LeaderEndorser
)

// EndorserConfig contains configuration for the endorser
type EndorserConfig struct {
	Role           EndorserRole
	LeaderEndorser string // Address of the leader endorser
	EndorserID     string // Unique ID of this endorser
	ChannelID      string // Channel ID this endorser belongs to
}

// Endorser provides the Endorser service ProcessProposal
type Endorser struct {
	ChannelFetcher         ChannelFetcher
	LocalMSP               msp.IdentityDeserializer
	PrivateDataDistributor PrivateDataDistributor
	Support                Support
	PvtRWSetAssembler      PvtRWSetAssembler
	Metrics                *Metrics
	Config                 EndorserConfig
	ShardManager           *sharding.ShardManager
	stopChan               chan struct{}
	wg                     sync.WaitGroup

	// Legacy fields for backward compatibility
	VariableMap               map[string]TransactionDependencyInfo
	VariableMapLock           sync.RWMutex
	EndorsementExpiryDuration time.Duration
	TxChannel                 chan *pb.ProposalResponse
	ResponseChannel           chan *pb.ProposalResponse
	ProcessingTxs             map[string]*pb.ProposalResponse
	ProcessingLock            sync.RWMutex
	HealthStatus              *HealthStatus
	HealthCheckLock           sync.RWMutex
	LastLeaderCheck           time.Time
	LeaderCheckError          error
	LeaderCircuitBreaker      *CircuitBreaker
}

// NewEndorser creates a new instance of Endorser with the given dependencies
func NewEndorser(channelFetcher ChannelFetcher, localMSP msp.IdentityDeserializer,
	pvtDataDistributor PrivateDataDistributor, support Support,
	pvtRWSetAssembler PvtRWSetAssembler, metrics *Metrics, config EndorserConfig) *Endorser {
	endorser := &Endorser{
		ChannelFetcher:            channelFetcher,
		LocalMSP:                  localMSP,
		PrivateDataDistributor:    pvtDataDistributor,
		Support:                   support,
		PvtRWSetAssembler:         pvtRWSetAssembler,
		Metrics:                   metrics,
		Config:                    config,
		ShardManager:              sharding.NewShardManager(nil, metrics),
		stopChan:                  make(chan struct{}),
		VariableMap:               make(map[string]TransactionDependencyInfo),
		EndorsementExpiryDuration: sharding.DefaultExpiryDuration,
		TxChannel:                 make(chan *pb.ProposalResponse, 1000),
		ResponseChannel:           make(chan *pb.ProposalResponse, 1000),
		ProcessingTxs:             make(map[string]*pb.ProposalResponse),
		HealthStatus: &HealthStatus{
			IsHealthy:     true,
			LastCheckTime: time.Now(),
			Details:       make(map[string]interface{}),
		},
		LeaderCircuitBreaker: NewCircuitBreaker(DefaultCircuitBreakerConfig(), metrics),
	}

	// Start leader-specific goroutines if this is a leader endorser
	if config.Role == LeaderEndorser {
		endorser.wg.Add(2)
		go func() {
			defer endorser.wg.Done()
			endorser.cleanupExpiredDependencies()
		}()
		go func() {
			defer endorser.wg.Done()
			endorser.processTransactions()
		}()
	}

	// Start health check goroutine
	endorser.wg.Add(1)
	go func() {
		defer endorser.wg.Done()
		endorser.runHealthChecks()
	}()

	return endorser
}

// Shutdown gracefully stops the endorser
func (e *Endorser) Shutdown() {
	close(e.stopChan)
	e.wg.Wait()
	if e.ShardManager != nil {
		e.ShardManager.Shutdown()
	}
}

// ProcessProposal processes the Proposal
// Errors related to the proposal itself are returned with an error that results in a grpc error.
// Errors related to proposal processing (either infrastructure errors or chaincode errors) are returned with a nil error,
// clients are expected to look at the ProposalResponse response status code (e.g. 500) and message.
func (e *Endorser) ProcessProposal(ctx context.Context, signedProp *pb.SignedProposal) (*pb.ProposalResponse, error) {
	// start time for computing elapsed time metric for successfully endorsed proposals
	startTime := time.Now()
	e.Metrics.ProposalsReceived.Add(1)

	addr := util.ExtractRemoteAddress(ctx)
	logger.Debug("request from", addr)

	success := false

	up, err := UnpackProposal(signedProp)
	if err != nil {
		e.Metrics.ProposalValidationFailed.Add(1)
		logger.Warnw("Failed to unpack proposal", "error", err.Error())
		return &pb.ProposalResponse{Response: &pb.Response{Status: 500, Message: err.Error()}}, err
	}

	var channel *Channel
	if up.ChannelID() != "" {
		channel = e.ChannelFetcher.Channel(up.ChannelID())
		if channel == nil {
			return &pb.ProposalResponse{Response: &pb.Response{Status: 500, Message: fmt.Sprintf("channel '%s' not found", up.ChannelHeader.ChannelId)}}, nil
		}
	} else {
		channel = &Channel{
			IdentityDeserializer: e.LocalMSP,
		}
	}

	err = e.preProcess(up, channel)
	if err != nil {
		logger.Warnw("Failed to preProcess proposal", "error", err.Error())
		return &pb.ProposalResponse{Response: &pb.Response{Status: 500, Message: err.Error()}}, err
	}

	defer func() {
		meterLabels := []string{
			"channel", up.ChannelHeader.ChannelId,
			"chaincode", up.ChaincodeName,
			"success", strconv.FormatBool(success),
		}
		e.Metrics.ProposalDuration.With(meterLabels...).Observe(time.Since(startTime).Seconds())
	}()

	pResp, err := e.ProcessProposalSuccessfullyOrError(up)
	if err != nil {
		logger.Warnw("Failed to invoke chaincode", "channel", up.ChannelHeader.ChannelId, "chaincode", up.ChaincodeName, "error", err.Error())
		// Return a nil error since clients are expected to look at the ProposalResponse response status code (500) and message.
		return &pb.ProposalResponse{Response: &pb.Response{Status: 500, Message: err.Error()}}, nil
	}

	if pResp.Endorsement != nil || up.ChannelHeader.ChannelId == "" {
		// We mark the tx as successful only if it was successfully endorsed, or
		// if it was a system chaincode on a channel-less channel and therefore
		// cannot be endorsed.
		success = true

		// total failed proposals = ProposalsReceived-SuccessfulProposals
		e.Metrics.SuccessfulProposals.Add(1)
	}
	return pResp, nil
}

// ProcessProposalSuccessfullyOrError implements the core endorsement logic with sharding support
func (e *Endorser) ProcessProposalSuccessfullyOrError(up *UnpackedProposal) (*pb.ProposalResponse, error) {
	txParams := &ccprovider.TransactionParams{
		ChannelID:  up.ChannelHeader.ChannelId,
		TxID:       up.ChannelHeader.TxId,
		SignedProp: up.SignedProposal,
		Proposal:   up.Proposal,
	}

	logger := decorateLogger(logger, txParams)

	// Acquire transaction simulator if needed
	if acquireTxSimulator(up.ChannelHeader.ChannelId, up.ChaincodeName) {
		txSim, err := e.Support.GetTxSimulator(up.ChannelID(), up.TxID())
		if err != nil {
			return nil, err
		}

		// txsim acquires a shared lock on the stateDB. As this would impact the block commits (i.e., commit
		// of valid write-sets to the stateDB), we must release the lock as early as possible.
		// Hence, this txsim object is closed in simulateProposal() as soon as the tx is simulated and
		// rwset is collected before gossip dissemination if required for privateData. For safety, we
		// add the following defer statement and is useful when an error occur. Note that calling
		// txsim.Done() more than once does not cause any issue. If the txsim is already
		// released, the following txsim.Done() simply returns.
		defer txSim.Done()

		hqe, err := e.Support.GetHistoryQueryExecutor(up.ChannelID())
		if err != nil {
			return nil, err
		}

		txParams.TXSimulator = txSim
		txParams.HistoryQueryExecutor = hqe
	}

	// Get chaincode endorsement info
	cdLedger, err := e.Support.ChaincodeEndorsementInfo(up.ChannelID(), up.ChaincodeName, txParams.TXSimulator)
	if err != nil {
		return nil, errors.WithMessagef(err, "make sure the chaincode %s has been successfully defined on channel %s and try again", up.ChaincodeName, up.ChannelID())
	}

	// Simulate the proposal
	res, simulationResult, ccevent, ccInterest, err := e.simulateProposal(txParams, up.ChaincodeName, up.Input)
	if err != nil {
		return nil, errors.WithMessage(err, "error in simulation")
	}

	if res.Status >= shim.ERROR {
		return &pb.ProposalResponse{Response: res}, nil
	}

	hasDependency := false
	dependentTxID := ""
	maxCommitIndex := uint64(0)
	maxTerm := uint64(0)

	// ===== SHARDED RAFT-BASED DEPENDENCY RESOLUTION =====

	if txParams.TXSimulator != nil && !e.Support.IsSysCC(up.ChaincodeName) {
		// Extract transaction dependencies from simulation results
		simResults, err := txParams.TXSimulator.GetTxSimulationResults()
		if err != nil {
			return nil, errors.WithMessage(err, "error getting simulation results")
		}

		dependencies, err := e.extractTransactionDependencies(simResults)
		if err != nil {
			return nil, errors.WithMessage(err, "error extracting transaction dependencies")
		}

		// Identify all involved shards (namespaces) from dependencies
		involvedShards := make(map[string]map[string][]byte) // shardName -> writeSet
		for varKey, varValue := range dependencies {
			parts := strings.Split(varKey, ":")
			if len(parts) > 0 {
				namespace := parts[0]
				// Only consider actual chaincode namespaces
				if namespace != "" && !e.Support.IsSysCC(namespace) {
					if _, exists := involvedShards[namespace]; !exists {
						involvedShards[namespace] = make(map[string][]byte)
					}
					involvedShards[namespace][varKey] = varValue
				}
			}
		}

		// If the primary chaincode wasn't picked up (e.g. read only with no deps), ensure it's at least queried
		contractName := up.ChaincodeName
		if _, exists := involvedShards[contractName]; !exists {
			involvedShards[contractName] = make(map[string][]byte)
		}

		var wg sync.WaitGroup
		var mu sync.Mutex

		var shardErrors []error
		contactedShards := make([]*sharding.ShardLeader, 0, len(involvedShards))

		ctx, cancel := context.WithTimeout(context.Background(), DefaultPrepareTimeout)
		defer cancel()

		for shardName, writeSet := range involvedShards {
			// Defensive check in case the Endorser was initialized before ShardManager
			if e.ShardManager == nil {
				logger.Warningf("ShardManager is strangely nil! Initializing fallback ShardManager automatically.")
				e.ShardManager = sharding.NewShardManager(nil, nil)
			}

			shard, err := e.ShardManager.GetOrCreateShard(shardName)
			if err != nil {
				shardErrors = append(shardErrors, errors.WithMessagef(err, "failed to get shard %s", shardName))
				continue
			}

			contactedShards = append(contactedShards, shard)
			wg.Add(1)

			go func(sName string, s *sharding.ShardLeader, wSet map[string][]byte) {
				defer wg.Done()

				prepareReq := &sharding.PrepareRequest{
					TxID:      up.ChannelHeader.TxId,
					ShardID:   sName,
					ReadSet:   make(map[string][]byte),
					WriteSet:  wSet,
					Timestamp: time.Now(),
				}

				select {
				case s.ProposeC() <- prepareReq:
					logger.Debugf("Submitted prepare request for tx %s to shard %s", prepareReq.TxID, sName)
				case <-ctx.Done():
					mu.Lock()
					shardErrors = append(shardErrors, fmt.Errorf("timeout submitting to shard %s", sName))
					mu.Unlock()
					return
				}

				select {
				case proof := <-s.CommitC():
					if !e.verifyProof(proof) {
						mu.Lock()
						shardErrors = append(shardErrors, fmt.Errorf("invalid proof from shard %s", sName))
						mu.Unlock()
						return
					}

					mu.Lock()
					if proof.CommitIndex > 1 {
						hasDependency = true
					}
					if proof.CommitIndex > maxCommitIndex {
						maxCommitIndex = proof.CommitIndex
						maxTerm = proof.Term
					}
					mu.Unlock()
				case <-ctx.Done():
					mu.Lock()
					shardErrors = append(shardErrors, fmt.Errorf("timeout waiting for proof from shard %s", sName))
					mu.Unlock()
				}
			}(shardName, shard, writeSet)
		}

		wg.Wait()

		if len(shardErrors) > 0 {
			// Abort on all contacted shards
			for _, s := range contactedShards {
				s.HandleAbort(up.ChannelHeader.TxId)
			}
			return nil, errors.Errorf("failed to gather dependency proofs: %v", shardErrors)
		}
	}

	// Create chaincode event bytes
	cceventBytes, err := CreateCCEventBytes(ccevent)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal chaincode event")
	}

	// Create proposal response payload
	prpBytes, err := protoutil.GetBytesProposalResponsePayload(up.ProposalHash, res, simulationResult, cceventBytes, &pb.ChaincodeID{
		Name:    up.ChaincodeName,
		Version: cdLedger.Version,
	})
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create the proposal response")
	}

	meterLabels := []string{
		"channel", up.ChannelID(),
		"chaincode", up.ChaincodeName,
	}

	// Handle different response status codes
	switch {
	case res.Status >= shim.ERROR:
		return &pb.ProposalResponse{
			Response: res,
			Payload:  prpBytes,
			Interest: ccInterest,
		}, nil
	case up.ChannelID() == "":
		return &pb.ProposalResponse{Response: res}, nil
	case res.Status >= shim.ERRORTHRESHOLD:
		meterLabels = append(meterLabels, "chaincodeerror", strconv.FormatBool(true))
		e.Metrics.EndorsementsFailed.With(meterLabels...).Add(1)
		return &pb.ProposalResponse{Response: res}, nil
	}

	// Endorse the response
	escc := cdLedger.EndorsementPlugin
	endorsement, mPrpBytes, err := e.Support.EndorseWithPlugin(escc, up.ChannelID(), prpBytes, up.SignedProposal)
	if err != nil {
		meterLabels = append(meterLabels, "chaincodeerror", strconv.FormatBool(false))
		e.Metrics.EndorsementsFailed.With(meterLabels...).Add(1)
		return nil, errors.WithMessage(err, "endorsing with plugin failed")
	}

	// Include dependency and proof information in response message
	res.Message = fmt.Sprintf("%s; DependencyInfo:HasDependency=%v,DependentTxID=%s,ShardCommitIndex=%d,ProofTerm=%d",
		res.Message, hasDependency, dependentTxID, maxCommitIndex, maxTerm)

	return &pb.ProposalResponse{
		Version:     1,
		Endorsement: endorsement,
		Payload:     mPrpBytes,
		Response:    res,
		Interest:    ccInterest,
	}, nil
}

// verifyProof verifies a prepare proof from the shard
func (e *Endorser) verifyProof(proof *sharding.PrepareProof) bool {
	if proof == nil || proof.TxID == "" || proof.ShardID == "" {
		return false
	}

	// Verify signature (simplified - in production, use actual crypto verification)
	expectedSig := fmt.Sprintf("%s:%d:%s", proof.ShardID, proof.CommitIndex, proof.TxID)
	return string(proof.Signature) == expectedSig
}

// runHealthChecks periodically performs health checks
func (e *Endorser) runHealthChecks() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.performHealthCheck()
		}
	}
}

// performHealthCheck performs all health checks and updates the status
func (e *Endorser) performHealthCheck() {
	e.HealthCheckLock.Lock()
	defer e.HealthCheckLock.Unlock()

	status := &HealthStatus{
		IsHealthy:     true,
		LastCheckTime: time.Now(),
		Details:       make(map[string]interface{}),
	}

	// Check dependency map health
	e.VariableMapLock.RLock()
	mapSize := len(e.VariableMap)
	e.VariableMapLock.RUnlock()
	status.Details["dependencyMapSize"] = mapSize

	// Check leader connectivity for normal endorsers
	if e.Config.Role == NormalEndorser {
		if err := e.checkLeaderConnectivity(); err != nil {
			status.IsHealthy = false
			status.Details["leaderConnectivity"] = err.Error()
			e.LeaderCheckError = err
		} else {
			status.Details["leaderConnectivity"] = "ok"
			e.LeaderCheckError = nil
		}
	}

	// Check transaction processing channels
	if e.TxChannel == nil || e.ResponseChannel == nil {
		status.IsHealthy = false
		status.Details["channels"] = "transaction channels not initialized"
	} else {
		status.Details["channels"] = "ok"
	}

	// Update health status
	e.HealthStatus = status
	logger.Infof("Health check completed. Status: %v, Details: %v", status.IsHealthy, status.Details)
}

// checkLeaderConnectivity checks if the normal endorser can connect to the leader
func (e *Endorser) checkLeaderConnectivity() error {
	if time.Since(e.LastLeaderCheck) < 30*time.Second {
		return e.LeaderCheckError
	}

	if e.LeaderCircuitBreaker == nil {
		return nil
	}

	return e.LeaderCircuitBreaker.Execute(func() error {
		conn, err := grpc.Dial(
			e.Config.LeaderEndorser,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
			grpc.WithTimeout(5*time.Second),
		)
		if err != nil {
			return fmt.Errorf("failed to connect to leader: %v", err)
		}
		defer conn.Close()

		e.LastLeaderCheck = time.Now()
		return nil
	})
}

// GetHealthStatus returns the current health status of the endorser
func (e *Endorser) GetHealthStatus() *HealthStatus {
	e.HealthCheckLock.RLock()
	defer e.HealthCheckLock.RUnlock()
	return e.HealthStatus
}

// preProcess checks the tx proposal headers, uniqueness and ACL
func (e *Endorser) preProcess(up *UnpackedProposal, channel *Channel) error {
	err := up.Validate(channel.IdentityDeserializer)
	if err != nil {
		e.Metrics.ProposalValidationFailed.Add(1)
		return errors.WithMessage(err, "error validating proposal")
	}

	if up.ChannelHeader.ChannelId == "" {
		return nil
	}

	meterLabels := []string{
		"channel", up.ChannelHeader.ChannelId,
		"chaincode", up.ChaincodeName,
	}

	if _, err = e.Support.GetTransactionByID(up.ChannelHeader.ChannelId, up.ChannelHeader.TxId); err == nil {
		e.Metrics.DuplicateTxsFailure.With(meterLabels...).Add(1)
		return errors.Errorf("duplicate transaction found [%s]. Creator [%x]", up.ChannelHeader.TxId, up.SignatureHeader.Creator)
	}

	if !e.Support.IsSysCC(up.ChaincodeName) {
		if err = e.Support.CheckACL(up.ChannelHeader.ChannelId, up.SignedProposal); err != nil {
			e.Metrics.ProposalACLCheckFailed.With(meterLabels...).Add(1)
			return err
		}
	}

	return nil
}

// buildChaincodeInterest builds the ChaincodeInterest structure for discovery service
func (e *Endorser) buildChaincodeInterest(simResult *ledger.TxSimulationResults) (*pb.ChaincodeInterest, error) {
	policies, err := parseWritesetMetadata(simResult.WritesetMetadata)
	if err != nil {
		return nil, err
	}

	for _, nsrws := range simResult.PubSimulationResults.GetNsRwset() {
		if e.Support.IsSysCC(nsrws.Namespace) {
			continue
		}
		if _, ok := policies.policyRequired[nsrws.Namespace]; !ok {
			policies.add(nsrws.Namespace, "", true)
		}
	}

	for chaincode, collections := range simResult.PrivateReads {
		for collection := range collections {
			policies.add(chaincode, collection, true)
		}
	}

	ccInterest := &pb.ChaincodeInterest{}
	for chaincode, collections := range policies.policyRequired {
		if e.Support.IsSysCC(chaincode) {
			continue
		}
		for collection := range collections {
			ccCall := &pb.ChaincodeCall{
				Name: chaincode,
			}
			if collection == "" {
				keyPolicies := policies.sbePolicies[chaincode]
				if len(keyPolicies) > 0 {
					ccCall.KeyPolicies = keyPolicies
					if !policies.requireChaincodePolicy(chaincode) {
						ccCall.DisregardNamespacePolicy = true
					}
				} else if !policies.requireChaincodePolicy(chaincode) {
					continue
				}
			} else {
				ccCall.CollectionNames = []string{collection}
				ccCall.NoPrivateReads = !simResult.PrivateReads.Exists(chaincode, collection)
			}
			ccInterest.Chaincodes = append(ccInterest.Chaincodes, ccCall)
		}
	}

	return ccInterest, nil
}
