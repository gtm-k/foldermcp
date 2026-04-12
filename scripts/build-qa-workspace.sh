#!/usr/bin/env bash
# build-qa-workspace.sh — Downloads and arranges the ~10 GB QA workspace
# from the MANIFEST.yaml. Idempotent: re-running skips already-cloned repos
# and already-downloaded files.
#
# Usage: scripts/build-qa-workspace.sh [dest]
#   dest defaults to testdata/v3/qa-workspace
#
# Dependencies: yq, jq, git, curl, sha256sum, python3 (for gen-writer-vault.py)

set -euo pipefail

DEST="${1:-testdata/v3/qa-workspace}"
MANIFEST="${DEST}/MANIFEST.yaml"

if [ ! -f "${MANIFEST}" ]; then
    echo "ERROR: MANIFEST.yaml not found at ${MANIFEST}"
    exit 1
fi

command -v yq >/dev/null 2>&1 || { echo "ERROR: yq is required but not installed. Install via: go install github.com/mikefarah/yq/v4@latest"; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required but not installed."; exit 1; }

echo "Building QA workspace at ${DEST} ..."
echo "Manifest version: $(yq '.version' "${MANIFEST}")"
echo "Target size: $(yq '.total_size_mb' "${MANIFEST}") MB"
echo ""

# Iterate over each source in the manifest
count=$(yq '.sources | length' "${MANIFEST}")
for i in $(seq 0 $(( count - 1 ))); do
    name=$(yq ".sources[${i}].name" "${MANIFEST}")
    kind=$(yq ".sources[${i}].kind" "${MANIFEST}")
    dest_sub=$(yq ".sources[${i}].dest" "${MANIFEST}")
    dest_full="${DEST}/${dest_sub}"

    echo "==> [$(( i + 1 ))/${count}] ${name} (${kind}) → ${dest_full}"
    mkdir -p "${dest_full}"

    case "${kind}" in
        git)
            url=$(yq ".sources[${i}].url" "${MANIFEST}")
            ref=$(yq ".sources[${i}].ref" "${MANIFEST}")
            if [ -d "${dest_full}/.git" ]; then
                echo "    Already cloned, skipping."
            else
                git clone --depth 1 --branch "${ref}" "${url}" "${dest_full}"
            fi
            ;;

        generate)
            gen=$(yq ".sources[${i}].generator" "${MANIFEST}")
            if [ ! -x "${gen}" ]; then
                echo "    Making generator executable: ${gen}"
                chmod +x "${gen}" 2>/dev/null || true
            fi
            echo "    Running generator: ${gen} ${dest_full}"
            if [[ "${gen}" == *.py ]]; then
                python3 "${gen}" "${dest_full}"
            else
                bash "${gen}" "${dest_full}"
            fi
            ;;

        list)
            list_file=$(yq ".sources[${i}].list" "${MANIFEST}")
            if [ ! -f "${list_file}" ]; then
                echo "    WARNING: list file ${list_file} not found, skipping."
                continue
            fi
            while IFS= read -r line || [ -n "${line}" ]; do
                # Skip comments and blank lines
                [[ "${line}" =~ ^[[:space:]]*# ]] && continue
                [[ -z "${line}" ]] && continue

                url=$(echo "${line}" | awk '{print $1}')
                sha=$(echo "${line}" | awk '{print $2}')
                file="${dest_full}/$(basename "${url}")"

                if [ -f "${file}" ]; then
                    echo "    Already downloaded: $(basename "${file}")"
                else
                    echo "    Downloading: ${url}"
                    curl -L --fail -o "${file}" "${url}"
                fi

                # Verify SHA256 if provided
                if [ -n "${sha}" ] && [ "${sha}" != "null" ]; then
                    echo "${sha}  ${file}" | sha256sum -c - || {
                        echo "    ERROR: SHA256 mismatch for ${file}"
                        rm -f "${file}"
                        exit 1
                    }
                fi
            done < "${list_file}"
            ;;

        *)
            echo "    WARNING: unknown source kind '${kind}', skipping."
            ;;
    esac
    echo ""
done

# Print summary
echo "================================================="
echo "QA workspace built at ${DEST}"
du -sh "${DEST}" 2>/dev/null || echo "(du not available)"
echo "================================================="
