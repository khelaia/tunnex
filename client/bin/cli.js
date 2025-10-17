#!/usr/bin/env node
'use strict';
import minimist from 'minimist';
import WebSocket from 'ws';
import net from 'net';
import * as fs from "node:fs";
import * as path from "node:path";
import { createHttpInspector } from '../inspect/http-inspector.js';


const argv = minimist(process.argv.slice(2), {
    string: ['host', 'domain', 'sub', 'localHost', 'proto', 'add-token', 'token', 'token-file',],
    number: ['port','inspect-port'],
    boolean: ['insecure', 'help', 'h','inspect-http'],
    alias: {h: 'host', p: 'port'},
    default: {
        domain: 'khelaia.com',
        sub: 'tunnex',
        localHost: '127.0.0.1',
        proto: 'wss',
        insecure: false,
        'inspect-http': false,
        'inspect-port': 4444,
    }
});

const printHelp = () => {
    console.log(`
tunnex - HTTP tunnel

Usage:
  tunnex --host <name> --port <localPort> [options]

Options:
  --host <string>         Public sub-host key to register (required)
  --port <number>         Local TCP port to forward to (required)
  --domain <host[:port]>  Tunnex server domain/host (default: 127.0.0.1:8888)
  --sub <string>          Prefix for public base host (default: tunnex)
  --proto <wss|ws>        Protocol for WS control channel (default: wss)
  --localHost <host>      Local target host (default: 127.0.0.1)
  --insecure              Skip TLS verification for WSS (default: false)
  --token <string>        Token for X-Tunnex-Token header
  --token-file <path>     Read token from a file
  --add-token <string>    Write token to ./token.txt (0600) then exit
  -h, --help              Show this help

Token order: --token > --token-file > env TUNNEX_TOKEN > ./token.txt

Examples:
  tunnex --add-token abc123
  tunnex --host app --port 3000 --domain example.com --sub tunnex --proto wss
  tunnex --host app --port 3000 --domain 127.0.0.1:8888 --proto ws
`);
}

if (argv.help) {
    printHelp();
    process.exit(0);
}

if (argv['add-token']) {
    const p = path.resolve(process.cwd(), 'token.txt');
    fs.writeFileSync(p, argv['add-token'], {mode: 0o600});
    console.log(`[tunnex] token saved to ${p} (0600)`);
    process.exit(0);
}

const readToken = (filePath) => {
    try {
        const s = fs.readFileSync(filePath, 'utf8');
        return s.split(/\r?\n/)[0].trim();
    } catch {
        return undefined;
    }
}



const host = argv.host;
const localPort = Number(argv.port);
if (!host || !localPort) {
    console.error('[tunnex] Missing required flags.\n');
    printHelp();
    process.exit(1);
}

let token = argv.token
    ?? (argv['token-file'] ? readToken(argv['token-file']) : undefined)
    ?? (process.env.TUNNEX_TOKEN || undefined)
    ?? (fs.existsSync('token.txt') ? readToken('token.txt') : undefined);

if (!token) {
    console.error('[tunnex] No token found (header X-Tunnex-Token will be omitted).');
    console.error('          Tip: use --token, --token-file, env TUNNEX_TOKEN, or --add-token.\n');
    process.exit(1)
}

const baseHost = `${host}-${argv.sub}.${argv.domain}`;
const publicUrl = `https://${baseHost}`;
const wsURL = `${argv.proto}://${baseHost}/register?host=${encodeURIComponent(host)}`;

console.log(`[tunnex] local: ${argv.localHost}:${localPort}`);
console.log(`[tunnex] remote: ${publicUrl}`);


const enableInspect = argv['inspect-http'] !== false;
const insp = enableInspect ? createHttpInspector({ port: argv['inspect-port'] || 4040 }) : null;


const ws = new WebSocket(wsURL, {
    perMessageDeflate: false,
    rejectUnauthorized: !argv.insecure,
    headers: {
        "X-Tunnex-Token": token,
    },
});

const streams = new Map();

ws.on('open', () => {
    console.log('[tunnex] WS connected')
    setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
            ws.ping()
        }
    }, 5000)
});

ws.on('message', (data) => {
    const msg = data.toString();
    const first = msg.indexOf(':');
    if (first < 0) return;
    const type = msg.slice(0, first).trim();
    const rest = msg.slice(first + 1);
    const second = rest.indexOf(':');
    const streamId = (second === -1 ? rest : rest.slice(0, second)).trim();
    const payloadHex = (second === -1 ? '' : rest.slice(second + 1)).trim();

    if (type === 'registered') {
        const sock = net.createConnection({host: argv.localHost, port: localPort}, () => {
        });
        sock.on('data', (chunk) => {
            if (ws.readyState === WebSocket.OPEN) {
                ws.send(`msg:${streamId}:${chunk.toString('hex')}`);
            }
        });
        sock.on('close', () => ws.readyState === WebSocket.OPEN && ws.send(`close:${streamId}`));
        sock.on('error', () => {
            const http404 =
                "HTTP/1.1 404 Not Found\r\n" +
                "Content-Type: text/plain\r\n" +
                "Content-Length: 25\r\n" +
                "\r\n" +
                "client not found for host";
            const hex404 = Buffer.from(http404, "utf8").toString("hex");
            ws.send(`msg:${streamId}:${hex404}`);
            ws.readyState === WebSocket.OPEN && ws.send(`close:${streamId}`)
        });

        streams.set(streamId, {sock, reqBuffers:[]});
        return;
    }

    if (type === 'msg') {
        const stream = streams.get(streamId);
        if (!stream)return;
        const buf = Buffer.from(payloadHex, 'hex');
        stream.reqBuffers.push(buf);

        const sock = stream.sock
        if (sock && sock.writable) {
            try {
                sock.write(Buffer.from(payloadHex, 'hex'));
            } catch {
            }
        }
        return;
    }

    if (type === 'close') {
        finalizeRequest(streamId);
    }
});



ws.on('close', () => {
    console.log('[tunnex] WS closed');
    for (const [, s] of streams) {
        try {
            s.destroy();
        } catch {
        }
    }
    streams.clear();
});

ws.on('error', (e) => console.error('[tunnex] WS error:', e.message));

for (const sig of ['SIGINT', 'SIGTERM']) {
    process.on(sig, () => {
        try {
            ws.close();
        } catch {
        }
        for (const [, s] of streams) {
            try {
                s.destroy();
            } catch {
            }
        }
        process.exit(0);
    });
}

function finalizeRequest(streamId) {
    const s = streams.get(streamId);
    if (!s) return;
    try { s.sock?.destroy(); } catch {}
    streams.delete(streamId);

    if (insp) {
        const raw = Buffer.concat(s.reqBuffers);
        insp.pushRaw(streamId, raw);
    }
}
