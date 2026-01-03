using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.IO;
using System.Net.Http;
using System.Net;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using RestSharp;

class Program
{
    const int TIMEOUT = 2000; // milliseconds

    static async Task Main(string[] args)
    {
        string inputFile = args.Length > 0 ? args[0] : "../../urls.json";
        string outputFile = args.Length > 1 ? args[1] : "result.json";
        int threads = args.Length > 2 ? int.Parse(args[2]) : 20;

        var urls = JsonSerializer.Deserialize<List<UrlItem>>(File.ReadAllText(inputFile)) ?? new List<UrlItem>();

        var clients = new Dictionary<string, Func<string, Task<Result>>>
        {
            { "httpclient", RequestHttpClient },
            { "restsharp", RequestRestSharp }
        };

        var results = new Dictionary<string, List<Result>>();

        foreach (var client in clients)
        {
            results[client.Key] = await RunConcurrent(urls, client.Value, threads);
        }

        File.WriteAllText(outputFile, JsonSerializer.Serialize(results, new JsonSerializerOptions { WriteIndented = true }));
        Console.WriteLine($"[+] Results saved to {outputFile}");
    }

    // =====================
    // Helper Classes
    // =====================
    public class UrlItem
    {
        public string url { get; set; } = "";
    }

    public class Result
    {
        public string url { get; set; } = "";
        public string? response { get; set; }
        public string? error { get; set; }
    }

    static Result MakeResult(string url, string? response = null, string? error = null)
    {
        return new Result
        {
            url = url,
            response = response != null && response.Length > 400 ? response.Substring(0, 400) : response,
            error = error
        };
    }

    // =====================
    // HTTP Clients
    // =====================

    // HttpClient
    static async Task<Result> RequestHttpClient(string url)
    {
        try
        {
            using var handler = new HttpClientHandler
            {
                AllowAutoRedirect = false,
                ServerCertificateCustomValidationCallback = HttpClientHandler.DangerousAcceptAnyServerCertificateValidator
            };
            using var client = new HttpClient(handler) { Timeout = TimeSpan.FromMilliseconds(TIMEOUT) };
            var res = await client.GetAsync(url);
            var body = await res.Content.ReadAsStringAsync();
            return MakeResult(url, body);
        }
        catch (Exception ex)
        {
            return MakeResult(url, null, ex.Message);
        }
    }

    // RestSharp
static async Task<Result> RequestRestSharp(string url)
{
    try
    {
        var options = new RestClientOptions(url)
        {
            Timeout = TimeSpan.FromMilliseconds(TIMEOUT), // <-- use TimeSpan
            FollowRedirects = false,
            RemoteCertificateValidationCallback = (sender, cert, chain, sslPolicyErrors) => true
        };
        var client = new RestClient(options);
        var request = new RestRequest();
        var response = await client.ExecuteAsync(request);
        return MakeResult(url, response.Content);
    }
    catch (Exception ex)
    {
        return MakeResult(url, null, ex.Message);
    }
}


    // =====================
    // Concurrency Logic
    // =====================
    static async Task<List<Result>> RunConcurrent(List<UrlItem> urls, Func<string, Task<Result>> fn, int concurrency)
    {
        var results = new ConcurrentBag<Result>();
        using var sem = new SemaphoreSlim(concurrency);

        var tasks = new List<Task>();

        foreach (var urlItem in urls)
        {
            await sem.WaitAsync();
            tasks.Add(Task.Run(async () =>
            {
                try
                {
                    results.Add(await fn(urlItem.url));
                }
                finally
                {
                    sem.Release();
                }
            }));
        }

        await Task.WhenAll(tasks);
        return new List<Result>(results);
    }
}
