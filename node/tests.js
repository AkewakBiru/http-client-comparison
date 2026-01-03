#!/usr/bin/env node

import fs from 'fs';
import https from 'https';
import http from 'http';
import { URL } from 'url';
import { Worker, isMainThread, parentPort } from 'worker_threads';
import fetch from 'node-fetch';
import axios from 'axios';
import got from 'got';
import { request as undiciRequest } from 'undici';

const TIMEOUT = 2000; // milliseconds

// =====================
// Helper
// =====================
function makeResult(url, response = null, error = null) {
  return {
    url,
    response: response ? response.slice(0, 400) : null,
    error,
  };
}

// =====================
// HTTP clients
// =====================

// --- Node built-in http/https
async function requestNative(url) {
  return new Promise((resolve) => {
    try {
      const u = new URL(url);
      const lib = u.protocol === 'https:' ? https : http;

      const req = lib.get(
        u,
        {
          rejectUnauthorized: false, // disable SSL verification
          timeout: TIMEOUT,
        },
        (res) => {
          let data = '';
          res.on('data', (chunk) => (data += chunk));
          res.on('end', () => resolve(makeResult(url, data)));
        }
      );

      req.on('error', (err) => resolve(makeResult(url, null, `${err.name}: ${err.message}`)));
      req.on('timeout', () => {
        req.destroy();
        resolve(makeResult(url, null, 'TimeoutError: request timed out'));
      });
    } catch (err) {
      resolve(makeResult(url, null, `${err.name}: ${err.message}`));
    }
  });
}

// --- node-fetch
const CONNECT_TIMEOUT = 1500;  // fail early on slow connect

async function requestFetch(url) {
  const controller = new AbortController();
  const fetchTimeout = setTimeout(() => controller.abort(), TIMEOUT);

  // pick correct agent
  const isHttps = url.includes('https');
  const agent = isHttps
    ? new https.Agent({ rejectUnauthorized: false })
    : new http.Agent();

  // --- connect timeout wrapper
  const connectTimer = new Promise((_, reject) => {
    const t = setTimeout(() => {
      clearTimeout(fetchTimeout);
      reject(new Error('ConnectTimeoutError: connect timed out'));
    }, CONNECT_TIMEOUT);

    // cancel this timer when fetch completes
    controller.signal.addEventListener('abort', () => clearTimeout(t));
  });

  // --- actual fetch promise
  const fetchPromise = (async () => {
    try {
      const res = await fetch(url, {
        redirect: 'manual',
        agent,
        signal: controller.signal,
      });
      const body = await res.text();
      return makeResult(url, body);
    } catch (err) {
      return makeResult(url, null, `${err.name}: ${err.message}`);
    } finally {
      clearTimeout(fetchTimeout);
    }
  })();

  // race: whichever fails first → timeout
  try {
    return await Promise.race([fetchPromise, connectTimer]);
  } catch (err) {
    return makeResult(url, null, `${err.name}: ${err.message}`);
  }
}

// --- axios
async function requestAxios(url) {
  try {
    const res = await axios.get(url, {
      timeout: TIMEOUT,
      maxRedirects: 0,
      httpsAgent: new https.Agent({ rejectUnauthorized: false }),
      validateStatus: null,
    });
    return makeResult(url, res.data);
  } catch (err) {
    return makeResult(url, null, `${err.name}: ${err.message}`);
  }
}

// --- got
async function requestGot(url) {
  try {
    const res = await got(url, {
      timeout: {
        // request: TIMEOUT,
        lookup: 50,
        connect: 1000,
        secureConnect: 50,
        socket: 1000,
        send: 1000,
        response: TIMEOUT
      },
      followRedirect: false,
      https: { rejectUnauthorized: false },
    });
    return makeResult(url, res.body);
  } catch (err) {
    return makeResult(url, null, `${err.name}: ${err.message}`);
  }
}

// --- undici
import { request, Agent } from 'undici';

async function requestUndici(url) {
  try {
    const agent = new Agent({
      pipelining: 0,
      connectTimeout: 1000,
      connect: { rejectUnauthorized: false }, // disable SSL verification
    });

    const { statusCode, body } = await request(url, {
      maxRedirections: 0,
      dispatcher: agent,
    });

    const chunks = [];
    for await (const chunk of body) chunks.push(chunk);

    return makeResult(url, Buffer.concat(chunks).toString());
  } catch (err) {
    return makeResult(url, null, `${err.name}: ${err.message}`);
  }
}

// =====================
// Concurrency logic
// =====================
async function runConcurrent(urls, fn, concurrency = 20) {
  const results = [];
  const queue = [...urls];

  const workers = Array(concurrency)
    .fill(0)
    .map(async () => {
      while (queue.length) {
        const url = queue.shift();
        if (!url) break;
        const res = await fn(url);
        results.push(res);
      }
    });

  await Promise.all(workers);
  return results;
}

// =====================
// Main
// =====================
async function main() {
  const inputFile = process.argv[2] || './urls.json';
  const outputFile = process.argv[3] || 'result.json';
  const threads = parseInt(process.argv[4] || '20', 10);

  const urls = JSON.parse(fs.readFileSync(inputFile)).map((x) => x.url);

  const clients = {
    http_s: requestNative,
    node_fetch: requestFetch,
    axios: requestAxios,
    got: requestGot,
    undici: requestUndici,
  };

  const results = {};

  for (const [name, fn] of Object.entries(clients)) {

    console.log(`[*] running ${name} ...`);

    const start = Date.now();
    results[name] = await runConcurrent(urls, fn, threads);
    const elapsed = Date.now() - start;

    console.log(`[+] ${name} finished in ${elapsed} ms`);

    // results[name] = await runConcurrent(urls, fn, threads);
  }

  fs.writeFileSync(outputFile, JSON.stringify(results, null, 2));
  console.log(`[+] results saved to ${outputFile}`);
}

main();