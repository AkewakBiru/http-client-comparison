<?php
// php-http-tester.php
require 'vendor/autoload.php';

use GuzzleHttp\Client;
// use GuzzleHttp\Promise;
use GuzzleHttp\Promise\Utils;
use GuzzleHttp\Promise\Create;
use Symfony\Component\HttpClient\HttpClient;
use Symfony\Contracts\HttpClient\Exception\TransportExceptionInterface;
use Amp\Http\Client\HttpClientBuilder;
use Amp\Http\Client\Request;
// use Amp\Promise;
use Amp\Sync\LocalSemaphore;
use function Amp\async;
use function Amp\Promise\all as AmpAll;
use Amp\Promise as AmpPromise;
use GuzzleHttp\Promise as GuzzlePromise;


date_default_timezone_set('UTC');

// =====================
// Helper
// =====================
function make_result($url, $response = null, $error = null) {
    return [
        "url" => $url,
        "response" => $response ? substr($response, 0, 400) : null,
        "error" => $error,
    ];
}

// =====================
// Requesters
// =====================

function request_curl($url) {
    $ch = curl_init();
    curl_setopt_array($ch, [
        CURLOPT_URL => $url,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT => 2,
        CURLOPT_FOLLOWLOCATION => true,
        CURLOPT_SSL_VERIFYPEER => false,
        CURLOPT_SSL_VERIFYHOST => 0,
    ]);
    $body = curl_exec($ch);
    $err = curl_error($ch);
    // curl_close($ch);
    return make_result($url, $body, $err ?: null);
}

function request_guzzle_safe($url) {
    try {
        $client = new \GuzzleHttp\Client(['timeout' => 2, 'verify' => false]);

        // Async can throw immediately if URL is malformed
        try {
            $promise = $client->getAsync($url);
        } catch (\Throwable $e) {
            return make_result($url, null, get_class($e) . ": " . $e->getMessage());
        }

        // Wait for the response (also wrapped in try/catch)
        try {
            $res = $promise->wait();
            return make_result($url, (string)$res->getBody());
        } catch (\Throwable $e) {
            return make_result($url, null, get_class($e) . ": " . $e->getMessage());
        }
    } catch (\Throwable $e) {
        return make_result($url, null, get_class($e) . ": " . $e->getMessage());
    }
}


function request_symfony_safe($url) {
    $client = \Symfony\Component\HttpClient\HttpClient::create([
        'timeout' => 2,
        'verify_peer' => false,
        'verify_host' => false,
    ]);

    try {
        $resp = $client->request('GET', $url);
        $content = $resp->getContent(false); // don't throw on HTTP errors
        return make_result($url, $content);
    } catch (\Throwable $e) {
        return make_result($url, null, get_class($e) . ": " . $e->getMessage());
    }
}

use Amp\Future;
use function Amp\Future\await;
use Amp\Socket\ClientTlsContext;
use Amp\Http\Client\Interceptor\SetRequestTimeout;
use Amp\Http\Client\Connection\DefaultConnectionFactory;
use Amp\Http\Client\Connection\UnlimitedConnectionPool;
use Amp\Socket\ConnectContext;

function request_amphp_safe(array $urls, int $concurrency = 20): array
{
    $TLS = (new ClientTlsContext(''))
    ->withoutPeerVerification() // I believe this one acts like the SSL_VERIFYPEER option of curl
    ->withSecurityLevel(0); // And this one disables SSL verification completely

    $context = (new ConnectContext)
    ->withConnectTimeout(2)
    ->withTlsContext($TLS);

    $client = (new HttpClientBuilder)
    ->usingPool(new UnlimitedConnectionPool(new DefaultConnectionFactory(null, $context)))
    ->intercept(new SetRequestTimeout(2, 1))
    ->followRedirects(1)
    ->retry(0)
    ->build();

    $sem = new LocalSemaphore($concurrency);
    $futures = [];

    foreach ($urls as $url) {
        $futures[] = async(function () use ($client, $url, $sem) {
            // Acquire the semaphore
            $lock = $sem->acquire(); // DO NOT await() here — returns Lock

            try {
                // Await the HTTP request
                $response = $client->request(new Request($url));
                $body = $response->getBody()->buffer();

                return [
                    'url'      => $url,
                    'response' => $body,
                    'error'    => null,
                ];
            } catch (\Throwable $e) {
                return [
                    'url'      => $url,
                    'response' => null,
                    'error'    => $e->getMessage(),
                ];
            } finally {
                $lock->release(); // release semaphore when done
            }
        });
    }

    // Await all HTTP request futures
    $final = await($futures);
    return array_values($final);
}



function request_file_get_contents_safe($url) {
    $body = false;
    $error = null;

    // Create stream context with timeout and disabled SSL verification
    $context = stream_context_create([
        'http' => [
            'timeout' => 2, // seconds
        ],
        'ssl' => [
            'verify_peer' => false,
            'verify_peer_name' => false,
            'allow_self_signed' => true,
        ],
    ]);

    try {
        // Suppress warnings with @ – we'll detect failure via return value anyway
        $body = @file_get_contents($url, false, $context);

        if ($body === false) {
            // Check stream errors if available
            $streamError = error_get_last();
            if ($streamError && strpos($streamError['message'], 'file_get_contents') !== false) {
                $error = $streamError['message'];
            } else {
                $error = "file_get_contents failed (possibly timeout, network issue, or invalid URL)";
            }
        }
    } catch (ValueError $e) {
        // Specifically handles empty path or invalid URL string
        $error = "ValueError: " . $e->getMessage();
    } catch (Error $e) {
        // Other fatal errors
        $error = "Error: " . $e->getMessage();
    } catch (Exception $e) {
        // Any other exceptions
        $error = "Exception: " . $e->getMessage();
    }

    return make_result($url, $body !== false ? $body : null, $error);
}

function request_readfile_safe($url) {
    $content = null;
    $bytes = false;
    $error = null;

    // Stream context: timeout + disable SSL verification
    $context = stream_context_create([
        'http' => [
            'timeout' => 2, // seconds
        ],
        'ssl' => [
            'verify_peer' => false,
            'verify_peer_name' => false,
            'allow_self_signed' => true,
        ],
    ]);

    try {
        ob_start();
        $bytes = @readfile($url, false, $context); // false = don't use include_path
        $content = ob_get_clean();

        if ($bytes === false) {
            $lastError = error_get_last();
            $error = $lastError ? $lastError['message'] : "readfile failed (unknown reason, possibly timeout or network issue)";
        }
    } catch (Throwable $e) {
        ob_end_clean();
        $error = get_class($e) . ": " . $e->getMessage();
    }

    return make_result($url, $bytes !== false ? $content : null, $error);
}


// =====================
// Multi-request (cURL multi)
// =====================
function multi_curl(array $urls, $concurrency = 20) {
    $results = [];
    $mh = curl_multi_init();
    $handles = [];

    // Initialize first batch
    $slice = array_slice($urls, 0, $concurrency);
    foreach ($slice as $url) {
        $ch = curl_init();
        curl_setopt_array($ch, [
            CURLOPT_URL => $url,
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_TIMEOUT => 2,
            CURLOPT_FOLLOWLOCATION => true,
            CURLOPT_SSL_VERIFYPEER => false,
            CURLOPT_SSL_VERIFYHOST => 0,
        ]);
        curl_multi_add_handle($mh, $ch);
        $handles[spl_object_hash($ch)] = ['handle' => $ch, 'url' => $url];
    }

    do {
        curl_multi_exec($mh, $active);
        curl_multi_select($mh);
    } while ($active > 0);

    foreach ($handles as $info) {
        $ch = $info['handle'];
        $url = $info['url'];
        $body = curl_multi_getcontent($ch);
        $err = curl_error($ch);
        $results[] = make_result($url, $body, $err ?: null);
        curl_multi_remove_handle($mh, $ch);
        // curl_close($ch);
    }

    curl_multi_close($mh);
    return $results;
}

// =====================
// Main
// =====================
$inputFile = $argv[1] ?? '../urls.json';
$outputFile = $argv[2] ?? 'result.json';
$threads = $argv[3] ?? 20;

$data = json_decode(file_get_contents($inputFile), true);
$urls = array_column($data, 'url');

$results = [];
$libraries = ['curl', 'guzzle', 'symfony', 'amphp', 'readfile', 'file_get_contents'];

foreach ($libraries as $lib) {
    echo "[+] $lib ($threads concurrent, TLS verify OFF)\n";
    $start = microtime(true);

    if ($lib === 'curl') {
        $results[$lib] = multi_curl($urls, $threads);

    } elseif ($lib === 'guzzle') {
        $client = new Client(['timeout' => 2, 'verify' => false]);
        $promises = [];

        foreach ($urls as $url) {
            try {
                $promise = $client->getAsync($url);

                $promises[] = $promise->then(
                    fn($res) => make_result($url, (string)$res->getBody()),
                    fn($e) => make_result($url, null, get_class($e) . ": " . $e->getMessage())
                );

            } catch (\Throwable $e) {
                $promises[] = \GuzzleHttp\Promise\Create::promiseFor(
                    make_result($url, null, get_class($e) . ": " . $e->getMessage())
                );
            }
        }

        $rawResults = Utils::settle($promises)->wait();
        $results[$lib] = [];
        foreach ($rawResults as $r) {
            if (isset($r['value'])) {
                $results[$lib][] = $r['value'];
            } elseif (isset($r['reason'])) {
                $results[$lib][] = make_result("unknown", null, (string)$r['reason']);
            }
        }

    } elseif ($lib === 'symfony') {
        $results[$lib] = [];
        $client = HttpClient::create([
            'timeout' => 2,
            'verify_peer' => false,
            'verify_host' => false,
        ]);
        foreach ($urls as $url) {
            try {
                $resp = $client->request('GET', $url);
                $results[$lib][] = make_result($url, $resp->getContent(false));
            } catch (\Throwable $e) {
                $results[$lib][] = make_result($url, null, get_class($e) . ": " . $e->getMessage());
            }
        }

    } elseif ($lib === 'file_get_contents') {
        $results[$lib] = [];
        foreach ($urls as $url) {
            $results[$lib][] = request_file_get_contents_safe($url);
        }
    } elseif ($lib === 'readfile') {
        $results[$lib] = [];
        foreach ($urls as $url) {
            $results[$lib][] = request_readfile_safe($url);
        }
    }  elseif ($lib === 'amphp') {
        $results[$lib] = request_amphp_safe($urls, $threads);
        // $results[$lib] = [];
        // foreach ($rawResults as $r) {
        //     if (isset($r['value'])) {
        //         $results[$lib][] = $r['value'];
        //     } elseif (isset($r['reason'])) {
        //         $results[$lib][] = make_result("unknown", null, (string)$r['reason']);
        //     }
        // }
        // $results[$lib] = request_amphp_safe($urls, $threads);
    }

    echo "    done in " . (microtime(true) - $start) . "s\n";
    // print_r($results[$lib]); // debug output per library
}


file_put_contents($outputFile, json_encode($results, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_IGNORE));
echo "[+] All done, results saved to $outputFile\n";
