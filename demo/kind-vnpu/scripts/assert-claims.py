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

    share_ids = set()
    pools = set()
    for claim in claims:
        results = claim["status"]["allocation"]["devices"]["results"]
        assert len(results) == 1, results
        result = results[0]
        assert result["device"] == "npu-0-0", result
        consumed = result["consumedCapacity"]
        assert consumed["npu.project-hami.io/memory"] == "1Gi", consumed
        assert consumed["npu.project-hami.io/aicore"] == "50", consumed
        assert result["shareID"], result
        share_ids.add(result["shareID"])
        pools.add(result["pool"])

    assert len(share_ids) == 2, share_ids
    assert len(pools) == 1, pools
    print("claims_same_device=true")
    print("distinct_share_ids=true")
    print("same_pool=true")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
