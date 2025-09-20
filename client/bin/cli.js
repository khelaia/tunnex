#!/usr/bin/env node
'use strict';
import minimist from 'minimist';
import WebSocket from 'ws';
import net from 'net';

const argv = minimist(process.argv.slice(2), {
    string: ['host', 'domain', 'sub', 'localHost', 'proto'],
    number: ['port'],
    boolean: ['insecure'],
    alias: { h: 'host', p: 'port' },
    default: {
        domain: '127.0.0.1:8888',
        sub: 'tunnex',
        localHost: '127.0.0.1',
        proto: 'wss',
        insecure: false
    }
});

const host = argv.host;
const localPort = Number(argv.port);
if (!host || !localPort) {
    console.error('Usage: tunnex --host <token> --port <localPort> [--domain tunnex-server-example.com] [--sub tunnex] [--proto wss|ws]');
    process.exit(1);
}

const baseHost = `${host}-${argv.sub}.${argv.domain}`;
const publicUrl = `https://${baseHost}`;
const wsURL = `${argv.proto}://127.0.0.1:8888/register?host=${encodeURIComponent(host)}`;

console.log(`[tunnex] local: ${argv.localHost}:${localPort}`);
console.log(`[tunnex] remote: ${publicUrl}`);

const ws = new WebSocket(wsURL, {
    perMessageDeflate: false,
    rejectUnauthorized: !argv.insecure,
    headers: {
        "X-Tunnex-Token": "e1beb782f1f8bc174fe73f2194ee4b47d83bcc8f7257be088b59cb13d3f291d6",
    },
});

const streams = new Map();

ws.on('open', () => {
    console.log('[tunnex] WS connected')
    setInterval(()=>{
        if (ws.readyState === WebSocket.OPEN) {
            ws.ping()
        }
    },5000)
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
        const sock = net.createConnection({ host: argv.localHost, port: localPort }, () => {
            sock.on('data', (chunk) => {
                if (ws.readyState === WebSocket.OPEN) {
                    ws.send(`msg:${streamId}:${chunk.toString('hex')}`);
                }
            });
            sock.on('close', () => ws.readyState === WebSocket.OPEN && ws.send(`close:${streamId}`));
            sock.on('error', () => ws.readyState === WebSocket.OPEN && ws.send(`close:${streamId}`));
        });
        streams.set(streamId, sock);
        return;
    }

    if (type === 'msg') {
        const sock = streams.get(streamId);
        if (sock && sock.writable) {
            try { sock.write(Buffer.from(payloadHex, 'hex')); } catch {}
        }
        return;
    }

    if (type === 'close') {
        const sock = streams.get(streamId);
        try { sock?.destroy(); } catch {}
        streams.delete(streamId);
        return;
    }
});

ws.on('close', () => {
    console.log('[tunnex] WS closed');
    for (const [, s] of streams) { try { s.destroy(); } catch {} }
    streams.clear();
});

ws.on('error', (e) => console.error('[tunnex] WS error:', e.message));

for (const sig of ['SIGINT', 'SIGTERM']) {
    process.on(sig, () => {
        try { ws.close(); } catch {}
        for (const [, s] of streams) { try { s.destroy(); } catch {} }
        process.exit(0);
    });
}
