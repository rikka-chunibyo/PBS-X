#!/usr/bin/env bash
# Run as root ON the disposable PBS-X test VM, with the compiled Go test binary.
# All enrollment/device credentials stay on this VM. Existing hosts are untouched.
set -euo pipefail
if [[ $EUID != 0 || $# != 1 ]]; then echo 'Usage: sudo test-live-pbs.sh /path/to/mobilecore-live.test' >&2; exit 2; fi
binary=$(realpath -- "$1")
test_id="android-smoke-$(date -u +%Y%m%d-%H%M%S)"
umask 077
scratch=$(mktemp -d /var/tmp/pbsx-android-live.XXXXXX)
namespace_created=false
host_created=false
job_created=false
api() { proxmox-backup-debug api "$@" --output-format json; }
cleanup(){
    local result=$? failed=0
    if $job_created; then api delete "/config/host-backup/$test_id" >/dev/null || failed=1; fi
    if $host_created; then api delete "/config/host/$test_id" >/dev/null || failed=1; fi
    if $namespace_created; then api delete /admin/datastore/backup/namespace --ns "$test_id" --delete-groups true >/dev/null || failed=1; fi
    rm -rf -- "$scratch"
    if ((failed)); then echo "Cleanup requires attention for disposable resources: $test_id" >&2; exit 1; else echo "Removed disposable test host, job, namespace and credentials: $test_id"; fi
    return "$result"
}
trap cleanup EXIT
api create /admin/datastore/backup/namespace --name "$test_id" >/dev/null
namespace_created=true
api create /config/host --name "$test_id" --comment 'Disposable Android core integration test' >/dev/null
host_created=true
api create /config/host-backup --host "$test_id" --id "$test_id" --sources /selected-folders --store backup --ns "$test_id" --snapshot-mode direct >/dev/null
job_created=true
api create "/config/host/$test_id/enrollment-token" > "$scratch/bootstrap.json"
python3 - "$scratch" "$test_id" <<'PY'
import base64, hashlib, json, pathlib, ssl, sys
folder=pathlib.Path(sys.argv[1]); host=sys.argv[2]
token=json.loads((folder/'bootstrap.json').read_text())['token']
certificate=ssl.get_server_certificate(('127.0.0.1',8007))
fingerprint=hashlib.sha256(ssl.PEM_cert_to_DER_cert(certificate)).hexdigest()
values={'v':1,'host_id':host,'server_url':'https://127.0.0.1:8007','bootstrap_token':token,'server_ca_fingerprint':fingerprint}
code='pbsx1.'+base64.urlsafe_b64encode(json.dumps(values).encode()).decode().rstrip('=')
(folder/'enrollment').write_text(code)
PY
PBSX_ANDROID_TEST_ENROLLMENT="$scratch/enrollment" PBSX_ANDROID_TEST_NAMESPACE="$test_id" "$binary" -test.run '^TestLivePBS$' -test.v -test.timeout 90s
