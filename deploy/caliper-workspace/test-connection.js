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
        console.log('Got contract fabcar. Attempting a read-only transaction (queryAllCars)...');

        const result = await contract.evaluateTransaction('queryAllCars');
        console.log(`Transaction evaluation response: ${result.toString()}`);

        gateway.disconnect();
    } catch (error) {
        console.error(`Failed to connect: ${error}`);
        if (error.stack) console.error(error.stack);
    }
}

testConnection();
