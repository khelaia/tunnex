'use strict';
const WebSocket = require('ws');
const net = require('net');

const streams = new Map()

const ws = new WebSocket('ws://127.0.0.1:8888/start', {perMessageDeflate: false});


ws.on('open', () => {
    console.log("WS connected");
});

ws.on('message', (data) => {

    const msg = data.toString();
    const type = msg.split(":")[0].trim();
    const streamId = msg.split(":")[1].trim();

    switch (type) {
        case 'start':
            const sock = net.createConnection({host: '127.0.0.1', port: 3000}, ()=>{
                sock.on('data', (chunk) => {
                    const hex = Buffer.from(chunk).toString('hex');
                    ws.send(`msg:${streamId}:${hex}`);
                });
                sock.on('close', () => ws.send(`close:${streamId}`));
                sock.on('error', () => ws.send(`close:${streamId}`));
            });

            streams.set(streamId, sock)
            break;
        case 'msg':
            const hex = msg.split(":")[2].trim();
            try {
                const sock = streams.get(streamId)
                sock.write(Buffer.from(hex, 'hex'));
            } catch {
                console.error("cant write into local connection")
            }
            break
    }
});

ws.on('close', () => {
    console.log('ws closed')
});
ws.on('error', (e) => {
    console.error('ws error:', e.message)
});


