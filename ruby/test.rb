#!/usr/bin/env ruby

require 'json'
require 'net/http'
require 'uri'
require 'openssl'
require 'open-uri'
require 'timeout'

require 'typhoeus'
require 'async'
require 'httparty'
require 'faraday'

TIMEOUT = 2

# =====================
# Helper
# =====================
def make_result(url, response = nil, error = nil)
  {
    url: url,
    response: response ? response[0, 400] : nil,
    error: error
  }
end

# =====================
# Requesters
# =====================

# ---- Net::HTTP (threaded)
def request_net_http(url)
  uri = URI(url)

  http = Net::HTTP.new(uri.host, uri.port)
  http.use_ssl = (uri.scheme == "https")
  http.verify_mode = OpenSSL::SSL::VERIFY_NONE
  http.read_timeout = TIMEOUT
  http.open_timeout = TIMEOUT
  http.max_retries = 0

  req = Net::HTTP::Get.new(uri)

  res = http.request(req)
  make_result(url, res.body)
rescue => e
  make_result(url, nil, "#{e.class}: #{e.message}")
end

# ---- open-uri
def request_open_uri(url)
  body = URI.open(
    url,
    read_timeout: TIMEOUT,
    open_timeout: TIMEOUT,
    ssl_verify_mode: OpenSSL::SSL::VERIFY_NONE
  ).read

  make_result(url, body)
rescue => e
  make_result(url, nil, "#{e.class}: #{e.message}")
end

# ---- Faraday
def request_faraday(url)
  conn = Faraday.new(
    url: url,
    ssl: { verify: false },
    request: { timeout: TIMEOUT, open_timeout: TIMEOUT }
  )

  res = conn.get
  make_result(url, res.body)
rescue => e
  make_result(url, nil, "#{e.class}: #{e.message}")
end

# ---- Typhoeus
def request_typhoeus(urls, concurrency)
  hydra = Typhoeus::Hydra.new(max_concurrency: concurrency)
  results = []

  urls.each do |url|
    req = nil
    url = url.to_s.gsub("\0", "") # null character in input is invalid in this library so just replace it with ""
    begin
      req = Typhoeus::Request.new(
        url,
        timeout: TIMEOUT * 1000,
        connecttimeout_ms: TIMEOUT * 1000,
        followlocation: false,
        ssl_verifypeer: false,
        ssl_verifyhost: 0
      )

    rescue ArgumentError => e
      if e.message.include?("null byte")
        results << make_result(url, nil, "Bad URL: contains null byte")
      else
        results << make_result(url, nil, "Bad URL: #{e.message}")
      end
      next  # Skip this bad URL, continue with others
    rescue => e
      results << make_result(url, nil, "Request setup error: #{e.message}")
      next
    end

    req.on_complete do |response|
      if response.success?
        results << make_result(url, response.body)
      elsif response.timed_out?
        results << make_result(url, nil, "Timeout (connect/read)")
      elsif response.code == 0
        results << make_result(url, nil, response.return_message || "Connection failed")
      else
        results << make_result(url, nil, "HTTP #{response.code}")
      end
    end

    hydra.queue(req)
  end

  hydra.run
  results
end

require 'http'
require 'openssl'

def request_http_rb(urls, concurrency, timeout: 2)
  results = []
  mutex = Mutex.new

  # Shared HTTP client configuration
#   http = HTTP.timeout(
#     connect: timeout,
#     write: timeout,
#     read: timeout
#   )

  ctx = OpenSSL::SSL::SSLContext.new
  ctx.verify_mode = OpenSSL::SSL::VERIFY_NONE

  urls.each_slice(concurrency) do |slice|
    threads = slice.map do |url|
      Thread.new do
        begin
            res = HTTP.timeout(
                connect: timeout,
                write: timeout,
                read: timeout
                )
            .follow(max_hops: 0)
            .get(url, :ssl_context => ctx)
        #   res = http.get(url, :ssl_context => ctx)
          mutex.synchronize do
            results << make_result(url, res.body.to_s)
          end
        rescue => e
          mutex.synchronize do
            results << make_result(url, nil, "#{e.class}: #{e.message}")
          end
        end
      end
    end
    threads.each(&:join)
  end

  results
end

def request_httparty(urls, concurrency, timeout: 2)
  results = []
  mutex = Mutex.new

  urls.each_slice(concurrency) do |slice|
    threads = slice.map do |url|
      Thread.new do
        begin
          res = HTTParty.get(
            url,
            timeout: timeout,
            verify: false
          )

          body = res.body

          mutex.synchronize do
            results << make_result(url, body)
          end
        rescue => e
          mutex.synchronize do
            results << make_result(url, nil, "#{e.class}: #{e.message}")
          end
        end
      end
    end

    threads.each(&:join)
  end

  results
end

require 'rest-client'

def request_rest_client(urls, concurrency, timeout: 2)
  results = []
  mutex = Mutex.new

  urls.each_slice(concurrency) do |slice|
    threads = slice.map do |url|
      Thread.new do
        begin
          res = RestClient::Request.execute(
            method: :get,
            url: url,
            timeout: timeout,
            open_timeout: timeout,
            verify_ssl: OpenSSL::SSL::VERIFY_NONE
          )

          body = res.body

          mutex.synchronize do
            results << make_result(url, body)
          end
        rescue => e
          mutex.synchronize do
            results << make_result(url, nil, "#{e.class}: #{e.message}")
          end
        end
      end
    end

    threads.each(&:join)
  end

  results
end

# =====================
# Main
# =====================
input_file  = ARGV[0] || "../urls.json"
output_file = ARGV[1] || "result.json"
threads     = (ARGV[2] || 20).to_i

urls = JSON.parse(File.read(input_file)).map { |x| x["url"] }

results = {}
libraries = %w[net_http open_uri faraday typhoeus http.rb httparty rest_client]

libraries.each do |lib|
  puts "[+] #{lib} (#{threads} concurrent, TLS verify OFF)"
  start = Time.now

  case lib
  when "net_http"
    results[lib] = []
    urls.each_slice(threads) do |slice|
      workers = slice.map do |url|
        Thread.new { request_net_http(url) }
      end
      workers.each { |t| results[lib] << t.value }
    end

  when "open_uri"
    results[lib] = []
    urls.each_slice(threads) do |slice|
      workers = slice.map do |url|
        Thread.new { request_open_uri(url) }
      end
      workers.each { |t| results[lib] << t.value }
    end

  when "faraday"
    results[lib] = []
    urls.each_slice(threads) do |slice|
      workers = slice.map do |url|
        Thread.new { request_faraday(url) }
      end
      workers.each { |t| results[lib] << t.value }
    end

  when "typhoeus"
    results[lib] = request_typhoeus(urls, threads)

  when "http.rb"
    results[lib] = request_http_rb(urls, threads)

  when "httparty"
    results[lib] = request_httparty(urls, threads)

  when "rest_client"
    results[lib] = request_rest_client(urls, threads)
  end

  puts "    done in #{Time.now - start}s"
end

File.write(
  output_file,
  JSON.pretty_generate(results)
)

puts "[+] All done, results saved to #{output_file}"
