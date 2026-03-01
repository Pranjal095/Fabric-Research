const { Gateway, Wallets } = require('fabric-network');
const fs = require('fs');
const path = require('path');
const yaml = require('js-yaml');

async function testConnection() {
    try {
        const ccpPath = path.resolve(__dirname, 'connection-profile.yaml');
        const ccp = yaml.load(fs.readFileSync(ccpPath, 'utf8'));

        const wallet = await Wallets.newInMemoryWallet();
        const certPath = path.resolve(__dirname, '../crypto-config/peerOrganizations/org1.example.com/users/User1@org1.example.com/msp/signcerts/User1@org1.example.com-cert.pem');
        const keyPath = path.resolve(__dirname, '../crypto-config/peerOrganizations/org1.example.com/users/User1@org1.example.com/msp/keystore/priv_sk');

        const certificate = fs.readFileSync(certPath).toString();
        const privateKey = fs.readFileSync(keyPath).toString();

        await wallet.put('User1', {
            credentials: {
                certificate,
                privateKey,
            },
            mspId: 'Org1MSP',
            type: 'X.509',
        });

        const gateway = new Gateway();
        await gateway.connect(ccp, {
            wallet,
            identity: 'User1',
            discovery: { enabled: false, asLocalhost: false }
        });

        console.log('Gateway connected successfully!');

        const network = await gateway.getNetwork('mychannel');
        console.log('Got network mychannel successfully!');

        const contract = network.getContract('fabcar');
        const channel = network.getChannel();

        const peersToTest = [
            'peer0.org1.example.com',
            'peer3.org1.example.com',
            'peer6.org1.example.com'
        ];

        for (const peerName of peersToTest) {
            console.log(`\n--- Testing ${peerName} ---`);
            try {
                const peer = channel.getEndorser(peerName);
                if (!peer) {
                    console.log(`❌ Peer ${peerName} not found in channel discovery.`);
                    continue;
                }

                const transaction = contract.createTransaction('queryAllCars');
                transaction.setEndorsingPeers([peer]);

                const result = await transaction.evaluate();
                console.log(`✅ Success! Response: ${result.toString().substring(0, 100)}...`);
            } catch (err) {
                console.log(`❌ Failed on ${peerName}: ${err.message}`);
                if (err.responses && err.responses.length > 0) {
                    console.log(`Endorsement Error Details: ${err.responses[0].message}`);
                }
            }
        }

        gateway.disconnect();
    } catch (error) {
        console.error(`Failed to connect: ${error}`);
        if (error.stack) console.error(error.stack);
    }
}

testConnection();
