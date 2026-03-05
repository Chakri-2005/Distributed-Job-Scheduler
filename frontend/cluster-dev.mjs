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

// ─── Node Configuration ─────────────────────────────────────────────
const NODES = [
    { id: 'master', port: 8080, role: 'master' },
    { id: 'slave1', port: 8081, role: 'slave' },
    { id: 'slave2', port: 8082, role: 'slave' },
    { id: 'slave3', port: 8083, role: 'slave' },
    { id: 'slave4', port: 8084, role: 'slave' },
];

// ─── Detect Local Network IP ────────────────────────────────────────
// Prefers: Mobile Hotspot / Wi-Fi > Ethernet > Fallback
// Skips: virtual adapters (Docker, VirtualBox, WSL, Hyper-V, etc.)
function getLocalIP() {
    // Allow manual override via environment variable
    if (process.env.CLUSTER_IP) {
        return process.env.CLUSTER_IP;
    }

    const interfaces = networkInterfaces();

    // Virtual adapter names to skip
    const skipPatterns = [
        'virtualbox', 'vbox', 'vmware', 'vmnet', 'docker', 'veth',
        'wsl', 'hyper-v', 'bluetooth', 'loopback', 'vethernet',
        'pseudo', 'teredo', 'isatap', 'vpn'
    ];

    let bestIP = null;
    let fallbackIP = '127.0.0.1';

    for (const name of Object.keys(interfaces)) {
        const nameLower = name.toLowerCase();
        const isVirtual = skipPatterns.some(p => nameLower.includes(p));
        if (isVirtual) continue;

        for (const iface of interfaces[name] || []) {
            if (iface.family === 'IPv4' && !iface.internal) {
                // Hotspot IPs are strongly preferred (iOS is 172.20.10.x, Android is often 192.168.43.x or 192.168.137.x)
                if (iface.address.startsWith('172.20.10.') || iface.address.startsWith('192.168.43.') || iface.address.startsWith('192.168.137.')) {
                    return iface.address;
                }

                // Otherwise prefer standard private subnets
                if (!bestIP) {
                    bestIP = iface.address;
                } else if (iface.address.startsWith('192.168.') || iface.address.startsWith('10.') || iface.address.startsWith('172.')) {
                    bestIP = iface.address;
                }

                if (fallbackIP === '127.0.0.1') {
                    fallbackIP = iface.address;
                }
            }
        }
    }

    return bestIP || fallbackIP;
}

// ─── Check & Start Docker Infrastructure ─────────────────────────────
function checkInfrastructure() {
    console.log('\n🔍 Checking infrastructure...\n');

    try {
        const result = execSync('docker ps --format "{{.Names}}"', { encoding: 'utf-8' });
        const running = result.trim().split('\n').filter(Boolean);

        const zkRunning = running.includes('zookeeper');
        const pgRunning = running.includes('postgres');

        if (!zkRunning || !pgRunning) {
            console.log('⚙️  Starting ZooKeeper and PostgreSQL via Docker...\n');
            execSync('docker-compose up -d zookeeper postgres', {
                cwd: ROOT,
                stdio: 'inherit'
            });

            // Wait for PostgreSQL to be healthy
            console.log('\n⏳ Waiting for services to be healthy...');
            let retries = 0;
            while (retries < 30) {
                try {
                    const health = execSync(
                        'docker inspect --format="{{.State.Health.Status}}" postgres 2>&1',
                        { encoding: 'utf-8' }
                    ).trim();
                    if (health === 'healthy') {
                        console.log('✅ PostgreSQL is healthy');
                        break;
                    }
                } catch { /* Not ready yet */ }
                retries++;
                execSync('timeout /t 2 /nobreak >nul 2>&1 || sleep 2', { stdio: 'ignore' });
            }

            // Give ZooKeeper a moment to stabilize
            execSync('timeout /t 3 /nobreak >nul 2>&1 || sleep 3', { stdio: 'ignore' });
            console.log('✅ ZooKeeper is ready\n');
        } else {
            console.log('✅ ZooKeeper is running');
            console.log('✅ PostgreSQL is running\n');
        }
    } catch (err) {
        console.error('❌ Docker is required. Please install Docker Desktop and make sure it is running.');
        console.error('   Error: ' + err.message);
        process.exit(1);
    }
}

// ─── Build Go API Server ─────────────────────────────────────────────
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
        console.error('❌ Failed to build API server. Make sure Go is installed.');
        console.error('   Error: ' + err.message);
        process.exit(1);
    }
}

// ─── Build React Frontend ────────────────────────────────────────────
function buildFrontend() {
    console.log('🎨 Building frontend...\n');

    const distPath = path.join(FRONTEND_DIR, 'dist');

    // Install dependencies if node_modules doesn't exist
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

// ─── Start a Go API Server Node ──────────────────────────────────────
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

// ─── Main ────────────────────────────────────────────────────────────
async function main() {
    const localIP = getLocalIP();

    console.log('\n' + '═'.repeat(60));
    console.log('    ⚡ Distributed Job Scheduler — Cluster Startup');
    console.log('═'.repeat(60));
    console.log(`\n    📡 Detected IP: ${localIP}\n`);

    // Step 1: Start Docker infrastructure (ZooKeeper + PostgreSQL)
    checkInfrastructure();

    // Step 2: Build Go API server binary
    const binaryPath = buildApiServer();

    // Step 3: Build React frontend
    const distPath = buildFrontend();

    // Step 4: Start all 5 node processes
    console.log('🚀 Starting cluster nodes...\n');

    const processes = [];
    for (const node of NODES) {
        const proc = startNode(node, binaryPath, distPath, localIP);
        processes.push(proc);
        // Stagger startup to avoid ZooKeeper election conflicts
        await new Promise(resolve => setTimeout(resolve, 1500));
    }

    // Step 5: Wait for nodes to register with ZooKeeper and elect leader
    await new Promise(resolve => setTimeout(resolve, 3000));

    // Step 6: Print cluster table
    console.log('\n' + '═'.repeat(60));
    console.log('    Distributed Job Scheduler Cluster');
    console.log('═'.repeat(60));
    console.log('');
    console.log('    Node        URL');
    for (const node of NODES) {
        const prefix = node.role === 'master'
            ? '    👑 Master  '
            : `    🔧 Slave ${node.id.replace('slave', '')}  `;
        console.log(`${prefix}http://${localIP}:${node.port}`);
    }
    console.log('');
    console.log('═'.repeat(60));
    console.log('    📊 Open any URL above to access the dashboard');
    console.log('    🔄 Tasks sync in real-time across all nodes');
    console.log('    🌐 Accessible from any device on the same network');
    console.log('    ⏹️  Press Ctrl+C to stop the cluster');
    console.log('═'.repeat(60));
    console.log('');

    // ─── Graceful Shutdown ───────────────────────────────────────
    const shutdown = () => {
        console.log('\n\n⏹️  Shutting down cluster...');
        for (const proc of processes) {
            try { proc.kill(); } catch { /* already exited */ }
        }
        console.log('✅ All nodes stopped\n');
        process.exit(0);
    };

    process.on('SIGINT', shutdown);
    process.on('SIGTERM', shutdown);

    // Windows: handle Ctrl+C properly
    if (process.platform === 'win32') {
        const readline = await import('readline');
        const rl = readline.createInterface({ input: process.stdin });
        rl.on('SIGINT', shutdown);
    }
}

main().catch((err) => {
    console.error('Fatal error:', err.message);
    process.exit(1);
});
