#!/bin/sh
set -eu

app_dir=${APP_DIR:-/app}
state_dir=${STATE_DIR:-/var/lib/opencode-farm-gateway}
config_path=${CONFIG_PATH:-$app_dir/config.json}
listen_address=${LISTEN_ADDRESS:-0.0.0.0:8080}
binary_path=$state_dir/opencode-farm-gateway
fingerprint_path=$state_dir/source.sha256

mkdir -p "$state_dir"

if [ -f "$config_path" ]; then
    active_config=$config_path
else
    active_config=$app_dir/config.example.json
    printf '%s\n' "config.json not found; starting with config.example.json. Copy it to config.json and set real keys before sending API requests."
fi

source_fingerprint() {
    {
        go version
        go env GOOS GOARCH CGO_ENABLED
        find "$app_dir" \
            -path "$app_dir/.git" -prune -o \
            -type f \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) -print \
            | LC_ALL=C sort \
            | while IFS= read -r source_file; do
                sha256sum "$source_file"
            done
    } | sha256sum | awk '{print $1}'
}

current_fingerprint=$(source_fingerprint)
stored_fingerprint=""
if [ -f "$fingerprint_path" ]; then
    stored_fingerprint=$(cat "$fingerprint_path")
fi

if [ ! -x "$binary_path" ] || [ "$current_fingerprint" != "$stored_fingerprint" ]; then
    printf '%s\n' "Source changed or no cached binary exists; building opencode-farm-gateway..."
    temporary_binary=$state_dir/opencode-farm-gateway.new
    cd "$app_dir"
    go build -trimpath -o "$temporary_binary" ./
    mv "$temporary_binary" "$binary_path"
    printf '%s\n' "$current_fingerprint" > "$fingerprint_path"
    printf '%s\n' "Build completed and cached in the persistent Docker volume."
else
    printf '%s\n' "Source is unchanged; starting the cached opencode-farm-gateway binary."
fi

exec "$binary_path" -config "$active_config" -listen "$listen_address"
