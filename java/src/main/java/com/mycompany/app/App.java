import java.io.IOException;
import java.io.FileWriter;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.security.cert.X509Certificate;
import java.time.Duration;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.*;
import javax.net.ssl.*;
import java.net.Socket;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;
import org.apache.hc.client5.http.classic.methods.HttpGet;
import org.apache.hc.client5.http.impl.classic.CloseableHttpClient;
import org.apache.hc.client5.http.impl.classic.CloseableHttpResponse;
import org.apache.hc.client5.http.impl.classic.HttpClients;
import org.apache.hc.core5.ssl.SSLContextBuilder;
import org.apache.hc.client5.http.ssl.DefaultClientTlsStrategy;
import org.apache.hc.core5.ssl.TrustStrategy;
import java.util.function.Function;
import org.apache.hc.client5.http.impl.io.PoolingHttpClientConnectionManager;
import org.apache.hc.client5.http.impl.io.PoolingHttpClientConnectionManagerBuilder;
import org.apache.hc.core5.ssl.SSLContexts;
import org.apache.hc.client5.http.ssl.NoopHostnameVerifier;
import org.apache.hc.client5.http.ssl.SSLConnectionSocketFactoryBuilder;
import org.apache.hc.client5.http.config.ConnectionConfig.Builder;
import org.apache.hc.client5.http.config.ConnectionConfig;
import java.security.cert.X509Certificate;
import javax.net.ssl.X509TrustManager;

public class App {

    private static final int TIMEOUT_MS = 2000;

    static class Result {
        String url;
        String response;
        String error;

        Result(String url, String response, String error) {
            this.url = url;
            this.response = response != null && response.length() > 400 ? response.substring(0, 400) : response;
            this.error = error;
        }
    }

    // ------------------------------
    // Java 11 HttpClient
    // ------------------------------
    public static Result requestJavaHttpClient(String url) {
        try {
            TrustManager[] trustAllCerts = new TrustManager[]{
            new X509TrustManager() {
                public X509Certificate[] getAcceptedIssuers() {
                    return null;
                }
                public void checkClientTrusted(X509Certificate[] certs, String authType) {
                }
                public void checkServerTrusted(X509Certificate[] certs, String authType) {
                }
            }
        };

            SSLContext sslContext = SSLContext.getInstance("TLS");
            sslContext.init(null, trustAllCerts, new java.security.SecureRandom());

            HttpClient client = HttpClient.newBuilder()
                    .connectTimeout(Duration.ofMillis(TIMEOUT_MS))
                    .followRedirects(HttpClient.Redirect.NEVER)
                    .sslContext(sslContext)
                    .build();

            HttpRequest request = HttpRequest.newBuilder()
                    .uri(URI.create(url))
                    .timeout(Duration.ofMillis(TIMEOUT_MS))
                    .GET()
                    .build();

            HttpResponse<String> response = client.send(request, HttpResponse.BodyHandlers.ofString());
            return new Result(url, response.body(), null);

        } catch (Exception e) {
            return new Result(url, null, e.getClass().getSimpleName() + ": " + e.getMessage());
        }
    }

    // ------------------------------
    // Apache HttpClient
    // ------------------------------
    public static Result requestApacheHttpClient(String url) {
        try {
            TrustManager[] trustAllCerts = new TrustManager[]{
                new X509TrustManager() {
                    @Override
                    public void checkClientTrusted(java.security.cert.X509Certificate[] chain, String authType) {
                    }

                    @Override
                    public void checkServerTrusted(java.security.cert.X509Certificate[] chain, String authType) {
                    }

                    @Override
                    public java.security.cert.X509Certificate[] getAcceptedIssuers() {
                        return new java.security.cert.X509Certificate[]{};
                    }
                }
            };
            
            SSLContext sslContext = SSLContext.getInstance("SSL");
            sslContext.init(null, trustAllCerts, new java.security.SecureRandom());
            CloseableHttpClient httpClient = HttpClients.custom()
            .setConnectionManager(PoolingHttpClientConnectionManagerBuilder.create()
                .setSSLSocketFactory(SSLConnectionSocketFactoryBuilder.create()
                    // Optional: set a specific SSLContext
                    .setSslContext(sslContext)
                    .setHostnameVerifier(NoopHostnameVerifier.INSTANCE) // Disables verification
                    .build())
                .setDefaultConnectionConfig(ConnectionConfig.custom() // fixed a lot of lag due to connect taking time for invalid URIs
                        .setConnectTimeout(org.apache.hc.core5.util.Timeout.ofMilliseconds(TIMEOUT_MS))  // <--- CONNECT TIMEOUT
                        .build())
                .build())
            .build();
            // CloseableHttpClient httpClient = HttpClients.custom()
            //     .setHostnameVerifier(new NoopHostnameVerifier()).setSSLContext(sslContext).build();
            

            HttpGet request = new HttpGet(url);
            request.setConfig(org.apache.hc.client5.http.config.RequestConfig.custom()
                    .setResponseTimeout(TIMEOUT_MS, TimeUnit.MILLISECONDS)
                    .build());

            

            try (CloseableHttpResponse response = httpClient.execute(request)) {
                String body = new String(response.getEntity().getContent().readAllBytes());
                return new Result(url, body, null);
            }

        } catch (Exception e) {
            return new Result(url, null, e.getClass().getSimpleName() + ": " + e.getMessage());
        }
    }

    // ------------------------------
    // OkHttp
    // ------------------------------
    public static Result requestOkHttp(String url) {
        try {
            TrustManager[] trustAllCerts = new TrustManager[]{
                new X509TrustManager() {
                    @Override
                    public void checkClientTrusted(java.security.cert.X509Certificate[] chain, String authType) {
                    }

                    @Override
                    public void checkServerTrusted(java.security.cert.X509Certificate[] chain, String authType) {
                    }

                    @Override
                    public java.security.cert.X509Certificate[] getAcceptedIssuers() {
                        return new java.security.cert.X509Certificate[]{};
                    }
                }
            };
            
            SSLContext sslContext = SSLContext.getInstance("SSL");
            sslContext.init(null, trustAllCerts, new java.security.SecureRandom());

            OkHttpClient.Builder newBuilder = new OkHttpClient.Builder();
            newBuilder.sslSocketFactory(sslContext.getSocketFactory(), (X509TrustManager) trustAllCerts[0]);
            newBuilder.hostnameVerifier((hostname, session) -> true);
            OkHttpClient client = newBuilder
                .connectTimeout(Duration.ofMillis(TIMEOUT_MS))
                .readTimeout(Duration.ofMillis(TIMEOUT_MS))
                .followRedirects(false)
            .build();
            // OkHttpClient client = new OkHttpClient.Builder()
            //         .connectTimeout(Duration.ofMillis(TIMEOUT_MS))
            //         .readTimeout(Duration.ofMillis(TIMEOUT_MS))
            //         .followRedirects(false)
            //         .sslSocketFactory(insecureSSLContext().getSocketFactory(), new X509TrustManager() {
            //             public void checkClientTrusted(X509Certificate[] chain, String authType) {}
            //             public void checkServerTrusted(X509Certificate[] chain, String authType) {}
            //             public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
            //         })
            //         .build();

            Request request = new Request.Builder().url(url).build();
            try (Response response = client.newCall(request).execute()) {
                return new Result(url, response.body().string(), null);
            }
        } catch (Exception e) {
            return new Result(url, null, e.getClass().getSimpleName() + ": " + e.getMessage());
        }
    }

    // ------------------------------
    // Concurrency runner
    // ------------------------------
    public static List<Result> runConcurrent(List<String> urls, Function<String, Result> fn, int concurrency) throws InterruptedException {
        ExecutorService executor = Executors.newFixedThreadPool(concurrency);
        List<Future<Result>> futures = new ArrayList<>();

        for (String url : urls) {
            futures.add(executor.submit(() -> fn.apply(url)));
        }

        List<Result> results = new ArrayList<>();
        for (Future<Result> f : futures) {
            try {
                results.add(f.get());
            } catch (Exception e) {
                results.add(new Result(null, null, e.getMessage()));
            }
        }

        executor.shutdown();
        executor.awaitTermination(1, TimeUnit.MINUTES);
        return results;
    }

    // ------------------------------
    // Main
    // ------------------------------
    public static void main(String[] args) throws IOException, InterruptedException {
        String inputFile = args.length > 0 ? args[0] : "urls.json";
        String outputFile = args.length > 1 ? args[1] : "result.json";
        int threads = args.length > 2 ? Integer.parseInt(args[2]) : 20;

        Gson gson = new Gson();
        List<Map<String, String>> urlObjects = gson.fromJson(new String(java.nio.file.Files.readAllBytes(java.nio.file.Path.of(inputFile))), List.class);
        List<String> urls = new ArrayList<>();
        for (Map<String, String> u : urlObjects) urls.add(u.get("url"));

        Map<String, List<Result>> results = new LinkedHashMap<>();

        Instant start = Instant.now();  // Start timing
        results.put("java_httpclient", runConcurrent(urls, App::requestJavaHttpClient, threads));
        double seconds = Duration.between(start, Instant.now()).toMillis() / 1000.0;
        System.out.printf("[+] Java HttpClient requests took: %.3f sec%n", seconds);

        start = Instant.now();
        results.put("apache_httpclient", runConcurrent(urls, App::requestApacheHttpClient, threads));
        seconds = Duration.between(start, Instant.now()).toMillis() / 1000.0;
        System.out.printf("[+] Apache HttpClient requests took: %.3f sec%n", seconds);

        start = Instant.now();
        results.put("okhttp", runConcurrent(urls, App::requestOkHttp, threads));
        seconds = Duration.between(start, Instant.now()).toMillis() / 1000.0;
        System.out.printf("[+] Okhttp requests took: %.3f sec%n", seconds);

        try (FileWriter writer = new FileWriter(outputFile)) {
            gson = new GsonBuilder().setPrettyPrinting().create();
            gson.toJson(results, writer);
        }

        System.out.println("[+] results saved to " + outputFile);
    }
}
