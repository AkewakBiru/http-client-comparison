from recollapse import Recollapse
import json, codecs, re

# MODE_NORM - if you need to fuzz normalization too. For now i don't need it
# MODE_TRUNC - i am not seeing how i can use it yet
# MODE_CASE - for applying cases to each char in the wordlist
mode = [Recollapse.MODE_START, Recollapse.MODE_SEP, Recollapse.MODE_TERM, Recollapse.MODE_RE_META]
# gen-delims: ":" / "/" / "?" / "#" / "[" / "]" / "@"
# sub-delims: "!" / "$" / "&" / "'" / "(" / ")" "*" / "+" / "," / ";" / "="

# characters to put in place, a list of lists where the max size is 2 (start and end)
input = [
        [":"], ["/"], ["?"], ["#"], ["["], ["]"], ["@"], # gen-delims
        ["!"], ["$"], ["&"], ["'"], ["("], [")"], ["*"], ["+"], [","], [";"], ["="], # sub-delims
        ["\x00"], ["\x09", "\x0d"], ["\x1c", "\x20"], ["\x85"], ["\xa0"], # Ascii CTL chars
        ["\\"], ["."], ["{"], ["}"], ["|"] # miscellaneous
    ]

# recollapse 'http://user:pass@parent.com:81/p' -m 1,2,4,5 -r 0x0d,0x0d >> recollapse
"""
# generates a wordlist by taking the argument and a list of input to place in the argument
"""
def generateWordlist(arg: str, rng: list=input) -> list[str]:
    res = []
    for inp in input:
        start = 0
        end = 0
        if len(inp) == 1:
            start = ord(inp[0])
            end = ord(inp[0])
        elif len(inp) == 2:
            start = ord(inp[0])
            end = ord(inp[1])
        rEnc = Recollapse(modes=mode, encoding=Recollapse.ENCODING_URL, range=[start,end]) # encodes the chars
        res.extend(rEnc.generate(arg))
        rNenc = Recollapse(modes=mode, encoding=Recollapse.ENCODING_UNICODE, range=[start,end]) # use unicode to place ctl chars
        tmp = [unicode_unescape(url) for url in rNenc.generate(arg)] # unicode unescape select chars
        res.extend(tmp)
    return res

def unicode_unescape(arg: str) -> str:
    """
    Unescape \\uXXXX and \\UXXXXXXXX sequences in the string,
    leaving all other backslashes untouched.
    
    Example:
        'test\\u0040pass\\u00a0space\\n' 
        → 'test@pass\xa0space\\n'
    """
    def repl(match):
        escape_seq = match.group(0)
        try:
            return codecs.decode(escape_seq, 'unicode-escape')
        except ValueError:
            # If invalid (e.g. \\uXXX or malformed), return original
            return escape_seq

    # Matches \\uXXXX (4 hex digits) and \\UXXXXXXXX (8 hex digits)
    pattern = re.compile(r'(\\u[0-9a-fA-F]{4}|\\U[0-9a-fA-F]{8})')
    return pattern.sub(repl, arg)

def save_items_as_json(items_list, filename):
    formatted_data = [{"name": "", "url": url} for url in items_list]
    try:
        with open(filename, 'w', encoding='utf-8') as json_file:
            json.dump(formatted_data, json_file, indent=4)
        print(f"Successfully stored data in {filename}")
    except IOError as e:
        print(f"IO Error writing to file {filename}: {e}")


# final = generateWordlist("http://u:p@parent.com:81@child.org:80")
# use escape and then i get chars before and after it
escaped = generateWordlist("http://u:p@parent.com:81\\child.org:80")
res2 = generateWordlist("@[::1%lo0]:81\\[::1%25lo0]:80/p")
for i in range(len(res2)):
    res2[i] = f"http://u:p{res2[i]}"
escaped.extend(res2)
escaped = list(set(escaped)) # remove duplicates if any
save_items_as_json(escaped, "recollapse.json")

# i have an IPv6 address, since i put a lot on above it would be repetitive to do it again so i will only add
# `@[::1%lo0]:81` as input 
# http://u:p@[::1]:81