import argparse
import json
import threading
import queue
import ssl
import time
import warnings

# Silence SSL warnings (important for urllib3 / requests)
warnings.filterwarnings("ignore")

# =====================
# Result structure
# =====================

def make_result(url, resp=None, err=None):
    return {
        "url": url,
        "response": resp,
        "error": err,
    }

# =====================
# Requesters (ALL INSECURE)
# =====================

    # import requests
    # from requests.packages.urllib3.exceptions import InsecureRequestWarning
def request_requests(url):
    import requests
    from requests.adapters import HTTPAdapter
    from urllib3.util.retry import Retry

    session = requests.Session()

    retries = Retry(
        total=0,                # 🔴 no retries
        connect=0,
        read=0,
        redirect=0,
        status=0,
        raise_on_redirect=False,
        raise_on_status=False,
    )

    adapter = HTTPAdapter(max_retries=retries)

    session.mount("http://", adapter)
    session.mount("https://", adapter)

    try:
        r = session.get(
            url.strip(),
            timeout=2,            # 🔴 2 seconds total
            verify=False,
            allow_redirects=True,
        )
        return make_result(url, r.text[:400])
    except Exception as e:
        return make_result(url, err=str(e))



def request_httpx(url):
    import httpx
    try:
        with httpx.Client(
            follow_redirects=True,
            timeout=2,
            verify=False,               # 🔴 disable cert verification
        ) as client:
            r = client.get(url)
            return make_result(url, r.text[:400])
    except Exception as e:
        return make_result(url, err=str(e))


def request_urllib3(url):
    import urllib3
    # from urllib.parse import urlparse
    import ssl
    try:
        # parsed = urlparse(url)
        if url[0:len("https://")].lower() == "https://":
            http = urllib3.PoolManager(
                cert_reqs=ssl.CERT_NONE,
                assert_hostname=False,
                retries=False,
                timeout=urllib3.Timeout(connect=2, read=2),
            )
        else:
            http = urllib3.PoolManager(
                timeout=urllib3.Timeout(connect=2, read=2),
                retries=False
            )

        r = http.request("GET", url, redirect=True)
        body = r.data.decode("utf-8", errors="replace")
        return make_result(url, body[:400])

    except Exception as e:
        return make_result(url, err=str(e))



def request_urllib(url):
    import urllib.request

    try:
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        ctx.check_hostname = False      # 🔴 disable hostname verification
        ctx.verify_mode = ssl.CERT_NONE # 🔴 disable cert verification

        with urllib.request.urlopen(url, context=ctx, timeout=2) as r:
            body = r.read().decode("utf-8", errors="replace")
            return make_result(url, body[:400])
    except Exception as e:
        return make_result(url, err=str(e))

async def request_aiohttp(session, sem, url):
    import asyncio

    # url = url.strip()

    async with sem:
        try:
            async with session.get(url) as resp:
                body = await resp.read()
                return {
                    "url": url,
                    "response": body.decode("utf-8", errors="replace"),
                    "error": None,
                }
        except Exception as e:
            return {
                "url": url,
                "response": None,
                "error": f"{type(e).__name__}: {repr(e)}",
            }


async def run_aiohttp(urls, concurrency):
    import aiohttp
    import asyncio
    import ssl

    # 🔴 Disable TLS verification
    ssl_ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    ssl_ctx.check_hostname = False
    ssl_ctx.verify_mode = ssl.CERT_NONE

    timeout = aiohttp.ClientTimeout(
        total=2,        # 🔴 hard 2s timeout
    )

    connector = aiohttp.TCPConnector(
        ssl=ssl_ctx,
        limit=0,        # no internal limit; semaphore controls concurrency
        force_close=True,
    )

    sem = asyncio.Semaphore(concurrency)

    async with aiohttp.ClientSession(
        timeout=timeout,
        connector=connector,
        raise_for_status=False,
    ) as session:
        tasks = [
            request_aiohttp(session, sem, url)
            for url in urls
        ]
        return await asyncio.gather(*tasks)



REQUESTERS = {
    "requests": request_requests,
    "httpx": request_httpx,
    "urllib3": request_urllib3,
    "urllib": request_urllib,
    "aiohttp": None,
}

# =====================
# Worker pool
# =====================

def run_pool(urls, requester, threads):
    q = queue.Queue()
    results = []
    lock = threading.Lock()

    for u in urls:
        q.put(u)

    def worker():
        while True:
            try:
                url = q.get_nowait()
            except queue.Empty:
                return

            res = requester(url)
            with lock:
                results.append(res)
            q.task_done()

    workers = []
    for _ in range(threads):
        t = threading.Thread(target=worker, daemon=True)
        t.start()
        workers.append(t)

    for t in workers:
        t.join()

    return results

# =====================
# Main
# =====================

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--threads", type=int, default=20)
    parser.add_argument("--lib", default="all", choices=["all"] + list(REQUESTERS.keys()))
    parser.add_argument("--input", default="../urls.json")
    parser.add_argument("--output", default="result.json")
    args = parser.parse_args()

    with open(args.input, "r") as f:
        data = json.load(f)

    urls = [x["url"] for x in data]

    store = {}
    libs = REQUESTERS.keys() if args.lib == "all" else [args.lib]

    for lib in libs:
        print(f"[+] {lib} ({args.threads} threads, TLS verify OFF)")
        start = time.time()

        if lib == "aiohttp":
            import asyncio
            store[lib] = asyncio.run(
                run_aiohttp(urls, args.threads)
            )
        else:
            store[lib] = run_pool(
                urls,
                REQUESTERS[lib],
                args.threads
            )

        print(f"    done in {time.time() - start:.2f}s")

    with open(args.output, "w") as f:
        json.dump(store, f, indent=2)


if __name__ == "__main__":
    main()
