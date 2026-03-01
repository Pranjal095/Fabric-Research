const grpc = require('@grpc/grpc-js');
const fs = require('fs');
const path = require('path');

const target = '10.96.1.87:7051';
const hostOverride = 'peer3.org1.example.com';
const rootCertPath = path.join(__dirname, 'crypto-config/peerOrganizations/org1.example.com/peers/peer3.org1.example.com/tls/ca.crt');

console.log(`Testing gRPC connection to ${target} with hostname override ${hostOverride}`);
console.log(`Using root cert from: ${rootCertPath}`);

try {
    const rootCertInfo = fs.readFileSync(rootCertPath);
    console.log("Certificate loaded successfully.");

    const credentials = grpc.credentials.createSsl(rootCertInfo);
    console.log("Credentials established.");

    const client = new grpc.Client(target, credentials, {
        'grpc.ssl_target_name_override': hostOverride,
        'grpc.default_authority': hostOverride
    });

    console.log("Client constructed, waiting for ready...");

    const deadline = new Date(Date.now() + 10000); // 10 seconds

    client.waitForReady(deadline, (error) => {
        if (error) {
            console.error('Failed to connect:', error.message);
        } else {
            console.log('Successfully connected!');
        }
        client.close();
    });
} catch (err) {
    console.error("Error during setup:", err);
}
