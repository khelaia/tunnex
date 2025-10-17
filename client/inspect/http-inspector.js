import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import crypto from 'node:crypto';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

function makeStore(limit = 500) {
    const items = [];
    const byId = new Map();
    const sse = new Set();

    const summarize = (x) => ({
        id: x.id,
        ts: x.ts,
        method: x.method,
        path: x.path,
        contentType: x.headers['content-type'] || null,
        bodySize: x.body.length
    });

    function push(rec) {
        if (items.length >= limit) {
            const old = items.shift();
            if (old) byId.delete(old.id);
        }
        items.push(rec);
        byId.set(rec.id, rec);
        broadcast({type: 'new', item: summarize(rec)});
    }

    function list() {
        return items.map(summarize);
    }

    function get(id) {
        return byId.get(id) || null;
    }

    function sub(res) {
        sse.add(res);
        res.on('close', () => sse.delete(res));
    }

    function broadcast(ev) {
        for (const r of sse) r.write(`data: ${JSON.stringify(ev)}\n\n`);
    }

    return {push, list, get, sub};
}

// ---- very small HTTP/1.x request parser (best-effort) ----
function parseHttpRequest(raw, maxBody = 64 * 1024) {
    if (!raw || raw.length === 0) return null;
    const headSep = raw.indexOf(Buffer.from('\r\n\r\n'));
    if (headSep === -1) return null;

    const headStr = raw.slice(0, headSep).toString('utf8');
    const body = raw.slice(headSep + 4, headSep + 4 + maxBody); // cap preview

    const lines = headStr.split('\r\n');
    const start = lines.shift() || '';
    const m = /^([A-Z]+)\s+(\S+)\s+HTTP\/\d\.\d$/.exec(start);
    if (!m) return null;

    const headers = {};
    for (const l of lines) {
        const i = l.indexOf(':');
        if (i > -1) headers[l.slice(0, i).trim().toLowerCase()] = l.slice(i + 1).trim();
    }
    return {method: m[1], path: m[2], headers, body};
}

export function createHttpInspector({
                                        port = 4040,
                                        host = '127.0.0.1',
                                        htmlPath = path.join(__dirname, 'inspector.html'),
                                        limit = 500,
                                    } = {}) {
    const store = makeStore(limit);

    const server = http.createServer((req, res) => {
        // static UI
        if (req.method === 'GET' && (req.url === '/' || req.url === '/inspect' || req.url.startsWith('/inspect/'))) {
            try {
                const html = fs.readFileSync(htmlPath, 'utf8');
                res.writeHead(200, {'Content-Type': 'text/html; charset=utf-8'});
                res.end(html);
            } catch (e) {
                res.writeHead(500, {'Content-Type': 'text/plain'});
                res.end('inspector.html not found');
            }
            return;
        }

        // list
        if (req.method === 'GET' && req.url === '/api/requests') {
            const json = JSON.stringify(store.list());
            res.writeHead(200, {'Content-Type': 'application/json'});
            res.end(json);
            return;
        }

        // details
        if (req.method === 'GET' && req.url.startsWith('/api/requests/')) {
            const id = decodeURIComponent(req.url.split('/').pop() || '');
            const rec = store.get(id);
            if (!rec) {
                res.writeHead(404);
                res.end('not found');
                return;
            }
            const payload = {
                id: rec.id,
                ts: rec.ts,
                method: rec.method,
                path: rec.path,
                headers: rec.headers,
                bodyPreview: rec.body.toString('utf8'), // already capped in parser
            };
            res.writeHead(200, {'Content-Type': 'application/json'});
            res.end(JSON.stringify(payload));
            return;
        }

        // SSE live stream
        if (req.method === 'GET' && req.url === '/events') {
            res.writeHead(200, {
                'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive',
            });
            res.write(':\n\n'); // open
            store.sub(res);
            return;
        }

        // 404
        res.writeHead(404, {'Content-Type': 'text/plain'});
        res.end('not found');
    });

    server.listen(port, host, () => {
        console.log(`[tunnex] Inspect UI http://${host}:${port}`);
    });

    // You call this from your tunnel code when a single HTTP request has been fully received (raw bytes)
    function pushRaw(streamId, rawBuffer) {
        const parsed = parseHttpRequest(rawBuffer);
        const id = streamId || crypto.randomBytes(8).toString('hex');
        const rec = {
            id,
            ts: Date.now(),
            method: parsed?.method || 'UNKNOWN',
            path: parsed?.path || '',
            headers: parsed?.headers || {},
            body: parsed?.body || Buffer.alloc(0),
        };
        store.push(rec);
    }

    return {pushRaw, close: () => server.close()};
}
