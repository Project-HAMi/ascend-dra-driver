#!/usr/bin/env python3

import json
import pathlib
import sys


def main():
    if len(sys.argv) != 2:
        print("usage: assert-claims.py CLAIMS_JSON", file=sys.stderr)
        return 2

    claims = json.loads(
        pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
    )["items"]
    assert len(claims) == 2, claims

    claims_by_name = {claim["metadata"]["name"]: claim for claim in claims}
    assert set(claims_by_name) == {"npu-share-a", "npu-share-b"}, claims_by_name

    results_by_claim = {
        claim_name: claim["status"]["allocation"]["devices"]["results"]
        for claim_name, claim in claims_by_name.items()
    }
    devices_by_claim = {
        claim_name: {result["device"] for result in results}
        for claim_name, results in results_by_claim.items()
    }
    assert len(results_by_claim["npu-share-a"]) == 1, results_by_claim[
        "npu-share-a"
    ]
    assert len(results_by_claim["npu-share-b"]) == 2, results_by_claim[
        "npu-share-b"
    ]
    assert len(devices_by_claim["npu-share-b"]) == 2, devices_by_claim[
        "npu-share-b"
    ]
    assert devices_by_claim["npu-share-a"] <= devices_by_claim["npu-share-b"], (
        devices_by_claim
    )

    share_ids = set()
    pools = set()
    for results in results_by_claim.values():
        for result in results:
            consumed = result["consumedCapacity"]
            assert consumed["memory"] == "1Gi", consumed
            assert consumed["cores"] == "50", consumed
            assert result["shareID"], result
            share_ids.add(result["shareID"])
            pools.add(result["pool"])

    assert len(share_ids) == 3, share_ids
    assert len(pools) == 1, pools
    print("claim_a_device_count=1")
    print("claim_b_device_count=2")
    print("claim_b_devices=" + ",".join(sorted(devices_by_claim["npu-share-b"])))
    print("claim_a_device_is_shared_with_b=true")
    print("distinct_share_ids=true")
    print("same_pool=true")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
