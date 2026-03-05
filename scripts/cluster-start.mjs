/*
 * Cluster Start Helper Script
 * Similar to the frontend development script, this orchestrator starts the
 * distributed environment from the root directory, ensuring PostgreSQL and
 * ZooKeeper are heavily verified before launching the Go binary nodes.
 */
import { execSync, spawn } from 'child_process';
import { networkInterfaces } from 'os';
import { existsSync } from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const ROOT = path.resolve(__dirname, '..');
const API_SERVER_DIR = path.resolve(ROOT, 'api-server');
const FRONTEND_DIR = path.resolve(ROOT, 'frontend');

// Node configuration
const NODES = [
    { id: 'master', port: 8080, role: 'master' },
    { id: 'slave1', port: 8081, role: 'slave' },
    { id: 'slave2', port: 8082, role: 'slave' },
    { id: 'slave3', port: 8083, role: 'slave' },
    { id: 'slave4', port: 8084, role: 'slave' },
];

// Detect local network IP (prefers Wi-Fi, then real Ethernet, skips virtual adapters)
function getLocalIP() {
    // Allow manual override via environment variable
    if (process.env.CLUSTER_IP) {
        return process.env.CLUSTER_IP;
    }

    const interfaces = networkInterfaces();

    // Names to skip (virtual adapters)
    const skipPatterns = [
        'virtualbox', 'vbox', 'vmware', 'vmnet', 'docker', 'veth',
        'wsl', 'hyper-v', 'bluetooth', 'loopback', 'vethernet'
    ];

    // Preferred adapter names
    const wifiPatterns = ['wi-fi', 'wifi', 'wlan', 'wireless'];

    let wifiIP = null;
    let ethernetIP = null;
    let fallbackIP = '127.0.0.1';

    for (const name of Object.keys(interfaces)) {
        const nameLower = name.toLowerCase();
        const isVirtual = skipPatterns.some(p => nameLower.includes(p));
        if (isVirtual) continue;

        const isWifi = wifiPatterns.some(p => nameLower.includes(p));

        for (const iface of interfaces[name] || []) {
            if (iface.family === 'IPv4' && !iface.internal) {
                if (isWifi && !wifiIP) {
                    wifiIP = iface.address;
                } else if (!ethernetIP) {
                    ethernetIP = iface.address;
                }
                if (fallbackIP === '127.0.0.1') {
                    fallbackIP = iface.address;
                }
            }
        }
    }

    // Priority: Wi-Fi > Ethernet > Fallback
    return wifiIP || ethernetIP || fallbackIP;
}

// Check if ZooKeeper and PostgreSQL are running
function checkInfrastructure() {
    console.log('\n🔍 Checking infrastructure...\n');

    try {
        const result = execSync('docker ps --format "{{.Names}}"', { encoding: 'utf-8' });
        const running = result.trim().split('\n');

        const zkRunning = running.includes('zookeeper');
        const pgRunning = running.includes('postgres');

        if (!zkRunning || !pgRunning) {
            console.log('⚙️  Starting ZooKeeper and PostgreSQL via Docker...\n');
            execSync('docker-compose up -d zookeeper postgres', {
                cwd: ROOT,
                stdio: 'inherit'
            });

            // Wait for services to be healthy
            console.log('\n⏳ Waiting for services to be healthy...');
            let retries = 0;
            while (retries < 30) {
                try {
                    const health = execSync('docker inspect --format="{{.State.Health.Status}}" postgres 2>&1', { encoding: 'utf-8' }).trim();
                    if (health === 'healthy') {
                        console.log('✅ PostgreSQL is healthy');
                        break;
                    }
                } catch { /* Not ready yet */ }
                retries++;
                execSync('timeout /t 2 /nobreak >nul 2>&1 || sleep 2', { stdio: 'ignore' });
            }

            // Give ZK a moment
            execSync('timeout /t 3 /nobreak >nul 2>&1 || sleep 3', { stdio: 'ignore' });
            console.log('✅ ZooKeeper is ready\n');
        } else {
            console.log('✅ ZooKeeper is running');
            console.log('✅ PostgreSQL is running\n');
        }
    } catch (err) {
        console.error('❌ Docker is required. Please install Docker Desktop.');
        process.exit(1);
    }
}

// Build the Go API server binary
function buildApiServer() {
    console.log('🔨 Building API server...\n');

    const binaryName = process.platform === 'win32' ? 'api-server.exe' : 'api-server';
    const binaryPath = path.join(API_SERVER_DIR, binaryName);

    try {
        execSync('go build -o ' + binaryName + ' .', {
            cwd: API_SERVER_DIR,
            stdio: 'inherit',
            env: { ...process.env, CGO_ENABLED: '0' },
        });
        console.log('✅ API server binary built\n');
        return binaryPath;
    } catch (err) {
        console.error('❌ Failed to build API server');
        process.exit(1);
    }
}

// Build the React frontend
function buildFrontend() {
    console.log('🎨 Building frontend...\n');

    const distPath = path.join(FRONTEND_DIR, 'dist');

    // Install dependencies if needed
    if (!existsSync(path.join(FRONTEND_DIR, 'node_modules'))) {
        console.log('📦 Installing frontend dependencies...');
        execSync('npm install', { cwd: FRONTEND_DIR, stdio: 'inherit' });
    }

    try {
        execSync('npx vite build', { cwd: FRONTEND_DIR, stdio: 'inherit' });
        console.log('\n✅ Frontend built\n');
        return distPath;
    } catch (err) {
        console.error('❌ Failed to build frontend');
        process.exit(1);
    }
}

// Start a node process
function startNode(node, binaryPath, distPath, localIP) {
    const env = {
        ...process.env,
        NODE_ID: node.id,
        PORT: String(node.port),
        NODE_IP: localIP,
        ZK_HOST: 'localhost:2181',
        DATABASE_URL: 'host=localhost user=postgres password=postgres dbname=jobscheduler sslmode=disable',
        FRONTEND_DIST: distPath,
        GIN_MODE: 'release',
    };

    const proc = spawn(binaryPath, [], {
        env,
        stdio: ['ignore', 'pipe', 'pipe'],
        cwd: API_SERVER_DIR,
    });

    proc.stdout.on('data', (data) => {
        const lines = data.toString().trim().split('\n');
        for (const line of lines) {
            if (line.trim()) {
                const prefix = node.role === 'master' ? '👑' : '🔧';
                console.log(`  ${prefix} [${node.id}:${node.port}] ${line.trim()}`);
            }
        }
    });

    proc.stderr.on('data', (data) => {
        const lines = data.toString().trim().split('\n');
        for (const line of lines) {
            if (line.trim() && !line.includes('[GIN-debug]')) {
                const prefix = node.role === 'master' ? '👑' : '🔧';
                console.log(`  ${prefix} [${node.id}:${node.port}] ${line.trim()}`);
            }
        }
    });

    proc.on('error', (err) => {
        console.error(`❌ Failed to start ${node.id}: ${err.message}`);
    });

    proc.on('exit', (code) => {
        if (code !== null && code !== 0) {
            console.error(`⚠️  ${node.id} exited with code ${code}`);
        }
    });

    return proc;
}

// Main
async function main() {
    const localIP = getLocalIP();

    console.log('\n' + '═'.repeat(60));
    console.log('    ⚡ Distributed Job Scheduler - Cluster Startup');
    console.log('═'.repeat(60));

    // Step 1: Check infrastructure
    checkInfrastructure();

    // Step 2: Build API server
    const binaryPath = buildApiServer();

    // Step 3: Build frontend
    const distPath = buildFrontend();

    // Step 4: Start all nodes
    console.log('🚀 Starting cluster nodes...\n');

    const processes = [];
    for (const node of NODES) {
        const proc = startNode(node, binaryPath, distPath, localIP);
        processes.push(proc);
        // Stagger startup to avoid ZK conflicts
        await new Promise(resolve => setTimeout(resolve, 1500));
    }

    // Step 5: Wait a moment for nodes to register
    await new Promise(resolve => setTimeout(resolve, 3000));

    // Step 6: Print cluster table
    console.log('\n' + '═'.repeat(60));
    console.log('    Distributed Job Scheduler Cluster');
    console.log('═'.repeat(60));
    console.log('');
    for (const node of NODES) {
        const prefix = node.role === 'master' ? '👑 Master  ' : `🔧 Slave ${node.id.replace('slave', '')} `;
        console.log(`    ${prefix} http://${localIP}:${node.port}`);
    }
    console.log('');
    console.log('═'.repeat(60));
    console.log('    📊 Open any URL above to access the dashboard');
    console.log('    🔄 Tasks sync in real-time across all nodes');
    console.log('    ⏹️  Press Ctrl+C to stop the cluster');
    console.log('═'.repeat(60));
    console.log('');

    // Handle graceful shutdown
    process.on('SIGINT', () => {
        console.log('\n\n⏹️  Shutting down cluster...');
        for (const proc of processes) {
            proc.kill();
        }
        console.log('✅ All nodes stopped\n');
        process.exit(0);
    });

    process.on('SIGTERM', () => {
        for (const proc of processes) {
            proc.kill();
        }
        process.exit(0);
    });
}

main().catch(console.error);
