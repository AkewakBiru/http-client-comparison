.PHONY: all go python3 node rust clean ruby php java dotnet wget curl

OUTFILE ?= result.json

INFILE ?= "/Users/akewakbiru/Documents/projects/url-requester-comparison/urls.json"

LANGS := go python3 node rust ruby php java dotnet curl wget

all: $(LANGS)

go:
	@echo "[+] Running Go tests"
	cd go && go run request.go $(INFILE) $(OUTFILE)

python3:
	@echo "[+] Running Python tests"
	cd python && source venv/bin/activate && python3 tests.py --input=$(INFILE) --output=$(OUTFILE) && deactivate

node:
	@echo "[+] Running node tests"
	cd node && node tests.js $(INFILE) $(OUTFILE) 20

php:
	@echo "[+] Running php tests"
	cd php && php tests.php $(INFILE) $(OUTFILE) 20

java:
	@echo "[+] Running java tests"
	@cd java && mvn -q clean compile exec:java -Dexec.args="$(INFILE) $(OUTFILE)"

ruby:
	@echo "[+] Running Ruby tests"
	cd ruby && ruby test.rb $(INFILE) $(OUTFILE)

rust:
	@echo "[+] Running Rust tests"
	cd rust && cargo run -- $(INFILE) $(OUTFILE)

dotnet:
	@echo "[+] Running Dotnet tests"
	cd dotnet/httpclient && dotnet run $(INFILE) $(OUTFILE)

curl:
	@echo "[+] Running curl tests"
	cd curl && go run main.go $(INFILE) $(OUTFILE)

wget:
	@echo "[+] Running wget tests"
	cd wget && go run main.go $(INFILE) $(OUTFILE)

clean:
	@echo "[+] Cleaning result.json files"
	rm -f go/result.json python/result.json node/result.json rust/result.json php/result.json java/result.json ruby/result.json 
		curl/result.sjon wget/result.json dotnet/result.json
