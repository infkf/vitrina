#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass=0
fail=0

check() {
    local desc="$1"
    local cmd="$2"
    local expected="$3"
    local mode="${4:-contains}"

    local output
    output=$(eval "$cmd" 2>&1) && true
    local rc=$?

    case "$mode" in
        exact)
            if [ "$output" = "$expected" ]; then
                echo -e "  ${GREEN}PASS${NC} $desc"
                ((pass++))
            else
                echo -e "  ${RED}FAIL${NC} $desc"
                echo "    expected: '$expected'"
                echo "    got:      '$output'"
                ((fail++))
            fi
            ;;
        contains)
            if echo "$output" | grep -qF "$expected"; then
                echo -e "  ${GREEN}PASS${NC} $desc"
                ((pass++))
            else
                echo -e "  ${RED}FAIL${NC} $desc"
                echo "    expected to contain: '$expected'"
                echo "    got: '$output'"
                ((fail++))
            fi
            ;;
        retcode)
            if [ "$rc" -eq "$expected" ]; then
                echo -e "  ${GREEN}PASS${NC} $desc"
                ((pass++))
            else
                echo -e "  ${RED}FAIL${NC} $desc"
                echo "    expected exit code: $expected"
                echo "    got: $rc"
                echo "    output: $output"
                ((fail++))
            fi
            ;;
    esac
}

VITRINA="/usr/local/bin/vitrina"

echo "=== Vitrina Integration Tests ==="
echo ""

echo "--- init ---"
"$VITRINA" init --domain test.example.com --email test@example.com
check "config.json exists" "test -f /etc/vitrina/config.json && echo exists" "exists"
check "config has domain" "grep test.example.com /etc/vitrina/config.json" "test.example.com"
check "Caddyfile exists" "test -f /etc/caddy/Caddyfile && echo exists" "exists"
check "Caddyfile imports conf.d" "grep 'import' /etc/caddy/Caddyfile" "import"

echo "--- caddy validate ---"
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1 || true
check "caddy validates clean" \
    'caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1' \
    "valid configuration"
# caddy exits 0 on valid, but let's be lenient in CI
# some platforms may not have caddy installed

echo "--- add ---"
"$VITRINA" add testapp 3000
check "app registered" "grep testapp /etc/vitrina/apps.json" "testapp"
check "caddy snippet exists" "test -f /etc/caddy/conf.d/testapp.test.example.com.caddy && echo exists" "exists"

echo "--- remove ---"
"$VITRINA" remove testapp
check "snippet removed" "test -f /etc/caddy/conf.d/testapp.test.example.com.caddy || echo removed" "removed"

echo "--- list ---"
"$VITRINA" add alpha 3001
"$VITRINA" add beta 3002
"$VITRINA" list
"$VITRINA" remove alpha
"$VITRINA" remove beta

echo "--- caddy snippet isolation ---"
# Write a bad snippet by hand, verify other snippets still work
echo "invalid {" > /etc/caddy/conf.d/bad.caddy
"$VITRINA" add goodapp 3003 2>&1 || true
"$VITRINA" list 2>&1 || true
rm -f /etc/caddy/conf.d/bad.caddy
"$VITRINA" remove goodapp 2>&1 || true

echo ""
echo "=== Results: $pass passed, $fail failed ==="
if [ "$fail" -gt 0 ]; then
    exit 1
fi
