use serde::{Deserialize, Serialize};
use std::{fs, time::Duration, sync::Arc, collections::HashMap};
use tokio::sync::Semaphore;
use futures::future::join_all;
use rayon::prelude::*;

#[derive(Serialize, Deserialize, Clone)]
struct UrlEntry {
    url: String,
}

#[derive(Serialize)]
struct ResultEntry {
    url: String,
    response: Option<String>,
    error: Option<String>,
}

const TIMEOUT_MS: u64 = 2000;

fn make_result(url: &str, response: Option<String>, error: Option<String>) -> ResultEntry {
    ResultEntry {
        url: url.to_string(),
        // We truncate the response to 400 chars for readability in the JSON
        response: response.map(|r| r.chars().take(400).collect()),
        error,
    }
}

// --- REQWEST (ASYNC) SECTION ---
async fn request_url_reqwest(client: &reqwest::Client, url: &str) -> ResultEntry {
    match client.get(url).send().await {
        Ok(resp) => match resp.text().await {
            Ok(text) => make_result(url, Some(text), None),
            Err(err) => make_result(url, None, Some(format!("Body error: {}", err))),
        },
        Err(err) => make_result(url, None, Some(format!("Req error: {}", err))),
    }
}

async fn run_reqwest(urls: Vec<String>, concurrency: usize) -> Vec<ResultEntry> {
    let semaphore = Arc::new(Semaphore::new(concurrency));
    let client = reqwest::Client::builder()
        .timeout(Duration::from_millis(TIMEOUT_MS))
        .danger_accept_invalid_certs(true)
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .unwrap();

    let mut tasks = Vec::new();
    for url in urls {
        let client = client.clone();
        let sem = semaphore.clone();
        tasks.push(tokio::spawn(async move {
            let _permit = sem.acquire_owned().await.unwrap();
            request_url_reqwest(&client, &url).await
        }));
    }

    join_all(tasks).await.into_iter().map(|r| r.unwrap()).collect()
}

// --- UREQ (SYNC 3.1.x) SECTION ---
fn request_url_ureq(agent: &ureq::Agent, url: &str) -> ResultEntry {
    match agent.get(url).call() {
        Ok(mut resp) => {
            // In ureq 3.1+, read_to_string() takes no arguments and returns Result<String>
            match resp.body_mut().read_to_string() {
                Ok(text) => make_result(url, Some(text), None),
                Err(err) => make_result(url, None, Some(format!("Body error: {}", err))),
            }
        }
        Err(err) => make_result(url, None, Some(format!("Req error: {}", err))),
    }
}

fn run_ureq(urls: Vec<String>, concurrency: usize) -> Vec<ResultEntry> {
    // Corrected configuration for ureq 3.1.x
    let config = ureq::Agent::config_builder()
        .timeout_global(Some(Duration::from_millis(TIMEOUT_MS)))
        .tls_config(
            ureq::tls::TlsConfig::builder()
                .disable_verification(true)
                .build()
        )
        .max_redirects(0) // Corrected method name
        .build();

    let agent = ureq::Agent::new_with_config(config);
    let agent = Arc::new(agent);

    let pool = rayon::ThreadPoolBuilder::new()
        .num_threads(concurrency)
        .build()
        .unwrap();

    pool.install(|| {
        urls.into_par_iter()
            .map(|url| request_url_ureq(&agent, &url))
            .collect()
    })
}

// --- MAIN ---
#[tokio::main]
async fn main() {
    let input_file = std::env::args().nth(1).unwrap_or("urls.json".to_string());
    let output_file = std::env::args().nth(2).unwrap_or("result.json".to_string());
    let threads = std::env::args()
        .nth(3)
        .unwrap_or("20".to_string())
        .parse::<usize>()
        .unwrap_or(20);

    let file_content = fs::read_to_string(&input_file).expect("Failed to read input file");
    let url_entries: Vec<UrlEntry> = serde_json::from_str(&file_content).expect("JSON error");
    let urls: Vec<String> = url_entries.into_iter().map(|x| x.url).collect();

    let mut results_map: HashMap<String, Vec<ResultEntry>> = HashMap::new();

    println!("[*] Running Reqwest (Async)...");
    let reqwest_res = run_reqwest(urls.clone(), threads).await;
    results_map.insert("reqwest".to_string(), reqwest_res);

    println!("[*] Running Ureq (Sync/Threadpool)...");
    let ureq_urls = urls.clone();
    let ureq_res = tokio::task::spawn_blocking(move || run_ureq(ureq_urls, threads))
        .await
        .unwrap();
    results_map.insert("ureq".to_string(), ureq_res);

    fs::write(&output_file, serde_json::to_string_pretty(&results_map).unwrap()).unwrap();
    println!("[+] Comparison complete. Results in {}", output_file);
}