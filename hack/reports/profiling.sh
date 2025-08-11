#!/usr/bin/env bash

set -eu -o pipefail
trap cleanup INT EXIT

PROJECT_ROOT="$(git rev-parse --show-toplevel)"

# config
declare DURATION=${DURATION:-30}
declare KEPLER_PORT=${KEPLER_PORT:-28282}
declare CPU_BASE_PROFILE=${CPU_BASE_PROFILE:-""}
declare MEM_BASE_PROFILE=${MEM_BASE_PROFILE:-""}
declare CPU_NEW_PROFILE=${CPU_NEW_PROFILE:-""}
declare MEM_NEW_PROFILE=${MEM_NEW_PROFILE:-""}
declare SHOW_HELP=false

# constants
declare -r PROJECT_ROOT
declare -r TMP_DIR="$PROJECT_ROOT/tmp"
declare -r CPU_PROFILE_DIR="$TMP_DIR/cpu-profile"
declare -r MEM_PROFILE_DIR="$TMP_DIR/mem-profile"
declare -r CPU_OUTPUT_DIR="$TMP_DIR/cpu-diff"
declare -r MEM_OUTPUT_DIR="$TMP_DIR/mem-diff"
declare -r KEPLER_BIN_DIR="$PROJECT_ROOT/bin"
declare -r CPU_HTTP_PORT=29000
declare -r MEM_HTTP_PORT=29001

source "$PROJECT_ROOT/hack/utils.bash"

cleanup() {
	info "Cleaning up ..."
	# Terminate all background jobs (e.g. pprof servers)
	{ jobs -p | xargs -I {} -- pkill -TERM -P {}; } || true
	wait
	sleep 1

	return 0
}
parse_args() {
	### while there are args parse them
	while [[ -n "${1+xxx}" ]]; do
		case $1 in
		--help | -h)
			shift
			SHOW_HELP=true
			return 0
			;;
		--base-cpu-profile)
			shift
			CPU_BASE_PROFILE="$1"
			shift
			;; # exit the loop
		--base-mem-profile)
			shift
			MEM_BASE_PROFILE="$1"
			shift
			;; # exit the loop
		--new-cpu-profile)
			shift
			CPU_NEW_PROFILE="$1"
			shift
			;; # exit the loop
		--new-mem-profile)
			shift
			MEM_NEW_PROFILE="$1"
			shift
			;;            # exit the loop
		*) return 1 ;; # show usage on everything else
		esac
	done
	return 0
}

print_usage() {
	local scr
	scr="$(basename "$0")"

	read -r -d '' help <<-EOF_HELP || true
		  🔆 Usage:
			  $scr <command> [OPTIONS]
			  $scr  -h | --help

		    📋 Commands:
		      capture        Captures a CPU and Memory profile
		      compare        Compares two profiles

		    💡 Examples:
		      →  $scr capture

		      →  $scr compare \\
		         --base-cpu-profile <Path to CPU profile> \\
		         --base-mem-profile <Path to Memory profile> \\
		         --new-cpu-profile <Path to CPU profile> \\
		         --new-mem-profile <Path to Memory profile>

			⚙️ Options:
		      -h | --help             Show this help message
		      --base-cpu-profile      Path to CPU profile to compare against
		      --base-mem-profile      Path to Memory profile to compare against
		      --new-cpu-profile       Path to CPU profile to compare
		      --new-mem-profile       Path to Memory profile to compare
	EOF_HELP

	echo -e "$help"
	return 0
}

profile_capture() {
	header "Running CPU Profiling"
	mkdir -p "$CPU_PROFILE_DIR"

	run go tool pprof -proto -seconds "$DURATION" \
		-output "$CPU_PROFILE_DIR"/profile.pb.gz "$KEPLER_BIN_DIR/kepler" \
		"http://localhost:$KEPLER_PORT/debug/pprof/profile" || return 1

	info "Start pprof web server in background"
	run go tool pprof --http "localhost:$CPU_HTTP_PORT" --no_browser \
		"$KEPLER_BIN_DIR/kepler" "$CPU_PROFILE_DIR/profile.pb.gz" </dev/null &
	sleep 1

	info "Fetch visualizations"
	for sample in {cpu,samples}; do
		curl --fail "http://localhost:$CPU_HTTP_PORT/ui/?si=$sample" -o "$CPU_PROFILE_DIR/graph-$sample.html" || return 1
		curl --fail "http://localhost:$CPU_HTTP_PORT/ui/flamegraph?si=$sample" -o "$CPU_PROFILE_DIR/flamegraph-$sample.html" || return 1
		for page in top peek source disasm; do
			curl --fail "http://localhost:$CPU_HTTP_PORT/ui/$page?si=$sample" -o "$CPU_PROFILE_DIR/$page-$sample.html" || return 1
		done
	done

	header "Running Memory Profiling"

	mkdir -p "$MEM_PROFILE_DIR"
	run go tool pprof -proto -seconds "$DURATION" \
		-output "$MEM_PROFILE_DIR"/profile.pb.gz "$KEPLER_BIN_DIR/kepler" \
		"http://localhost:$KEPLER_PORT/debug/pprof/heap" || return 1

	info "Start pprof web server in background"
	run go tool pprof --http "localhost:$MEM_HTTP_PORT" --no_browser \
		"$KEPLER_BIN_DIR/kepler" "$MEM_PROFILE_DIR/profile.pb.gz" </dev/null &
	sleep 1

	info "Fetch visualizations"
	for sample in {alloc,inuse}_{objects,space}; do
		curl --fail "http://localhost:$MEM_HTTP_PORT/ui/?si=$sample" -o "$MEM_PROFILE_DIR/graph-$sample.html" || return 1
		curl --fail "http://localhost:$MEM_HTTP_PORT/ui/flamegraph?si=$sample" -o "$MEM_PROFILE_DIR/flamegraph-$sample.html" || return 1
		for page in top peek source disasm; do
			curl --fail "http://localhost:$MEM_HTTP_PORT/ui/$page?si=$sample" -o "$MEM_PROFILE_DIR/$page-$sample.html" || return 1
		done
	done

	return 0
}

profile_compare() {
	header "Comparing CPU Profiles"
	[[ -z "$CPU_BASE_PROFILE" || -z "$CPU_NEW_PROFILE" ]] && {
		fail "Missing required inputs for CPU diff: base_profile=$CPU_BASE_PROFILE, new_profile=$CPU_NEW_PROFILE"
		return 1
	}

	mkdir -p "$CPU_OUTPUT_DIR"

	info "Start pprof web server for diff in background"
	run go tool pprof --http "localhost:$CPU_HTTP_PORT" --no_browser \
		-base "$CPU_BASE_PROFILE" "$KEPLER_BIN_DIR/kepler" "$CPU_NEW_PROFILE" </dev/null &
	sleep 1

	info "Fetch visualizations"
	for sample in {cpu,samples}; do
		curl --fail "http://localhost:$CPU_HTTP_PORT/ui/?si=$sample" -o "$CPU_OUTPUT_DIR/graph-$sample.html" || return 1
		curl --fail "http://localhost:$CPU_HTTP_PORT/ui/flamegraph?si=$sample" -o "$CPU_OUTPUT_DIR/flamegraph-$sample.html" || return 1
		for page in top peek source disasm; do
			curl --fail "http://localhost:$CPU_HTTP_PORT/ui/$page?si=$sample" -o "$CPU_OUTPUT_DIR/$page-$sample.html" || return 1
		done
	done

	header "Comparing Memory Profiles"
	[[ -z "$MEM_BASE_PROFILE" || -z "$MEM_NEW_PROFILE" ]] && {
		fail "Missing required inputs for MEM diff: base_profile=$MEM_BASE_PROFILE, new_profile=$MEM_NEW_PROFILE"
		return 1
	}

	mkdir -p "$MEM_OUTPUT_DIR"

	info "Start pprof web server for diff in background"
	run go tool pprof --http "localhost:$MEM_HTTP_PORT" --no_browser \
		-base "$MEM_BASE_PROFILE" "$KEPLER_BIN_DIR/kepler" "$MEM_NEW_PROFILE" </dev/null &
	sleep 1

	info "Fetch visualizations"
	for sample in {alloc,inuse}_{objects,space}; do
		curl --fail "http://localhost:$MEM_HTTP_PORT/ui/?si=$sample" -o "$MEM_OUTPUT_DIR/graph-$sample.html" || return 1
		curl --fail "http://localhost:$MEM_HTTP_PORT/ui/flamegraph?si=$sample" -o "$MEM_OUTPUT_DIR/flamegraph-$sample.html" || return 1
		for page in top peek source disasm; do
			curl --fail "http://localhost:$MEM_HTTP_PORT/ui/$page?si=$sample" -o "$MEM_OUTPUT_DIR/$page-$sample.html" || return 1
		done
	done

	return 0
}

main() {
	local fn=${1:-''}
	shift

	parse_args "$@" || die "failed to parse args"

	$SHOW_HELP && {
		print_usage
		exit 0
	}

	cd "$PROJECT_ROOT"

	local cmd_fn="profile_$fn"
	if ! is_fn "$cmd_fn"; then
		fail "unknown command: $fn"
		print_usage
		return 1
	fi

	$cmd_fn "$@" || return 1

	return 0
}

main "$@"
