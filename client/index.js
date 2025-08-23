'use strict';
const WebSocket = require('ws');
const net = require('net');


const ws = new WebSocket('ws://127.0.0.1:8888/start', {perMessageDeflate: false});

const sock = net.createConnection({host: '127.0.0.1', port: 3000});

ws.on('open', () => {
    console.log("WS connected");
});

ws.on('message', (data) => {
    const msg = data.toString();
    if (msg.startsWith('msg:')) {
        const hex = msg.split(":")[1].trim();
        if (sock) {
            try {
                sock.write(Buffer.from(hex, 'hex'));
            } catch {
                console.error("cant write into local connection")
            }
        }
    }
});

ws.on('close', () => {
    console.log('ws closed')
});
ws.on('error', (e) => {
    console.error('ws error:', e.message)
});

sock.on('data', (chunk) => {
    const hex = Buffer.from(chunk).toString('hex');
    ws.send(`msg:${hex}`);
});
sock.on('close', () => ws.send(`close`));
sock.on('error', () => ws.send(`close`));
