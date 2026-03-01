window.BENCHMARK_DATA = {
  "lastUpdate": 1772408750933,
  "repoUrl": "https://github.com/volchanskyi/opengate",
  "entries": {
    "Benchmark": [
      {
        "commit": {
          "author": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "committer": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "distinct": true,
          "id": "8457ea15c7b589bb1ed5e5a54c2a714a11a4979a",
          "message": "fix(ci): fix benchmark CI jobs — create gh-pages, fix output paths\n\nGo bench output written to $GITHUB_WORKSPACE/bench-go.txt to avoid\nworking-directory path issues. Rust bench stderr suppressed and output\nvalidated before passing to github-action-benchmark. Created gh-pages\nbranch for benchmark data storage.\n\nCo-Authored-By: Ivan Volchanskyi <ivan.volchanskyi@gmail.com>",
          "timestamp": "2026-03-01T13:27:28-08:00",
          "tree_id": "97c4f636b97a17ecfec6cae613737cf1e1677ee2",
          "url": "https://github.com/volchanskyi/opengate/commit/8457ea15c7b589bb1ed5e5a54c2a714a11a4979a"
        },
        "date": 1772400503027,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15471,
            "unit": "ns/op\t    5401 B/op\t      62 allocs/op",
            "extra": "76940 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15471,
            "unit": "ns/op",
            "extra": "76940 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5401,
            "unit": "B/op",
            "extra": "76940 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "76940 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156788,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7574 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156788,
            "unit": "ns/op",
            "extra": "7574 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7574 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7574 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 157584,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7611 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 157584,
            "unit": "ns/op",
            "extra": "7611 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7611 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7611 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 306398,
            "unit": "ns/op\t   28176 B/op\t     409 allocs/op",
            "extra": "3847 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 306398,
            "unit": "ns/op",
            "extra": "3847 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "3847 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3847 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52405,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22393 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52405,
            "unit": "ns/op",
            "extra": "22393 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22393 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22393 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 269889,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4603 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 269889,
            "unit": "ns/op",
            "extra": "4603 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4603 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4603 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20737,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56548 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20737,
            "unit": "ns/op",
            "extra": "56548 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56548 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56548 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 152035,
            "unit": "ns/op\t   41697 B/op\t    1978 allocs/op",
            "extra": "7497 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 152035,
            "unit": "ns/op",
            "extra": "7497 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41697,
            "unit": "B/op",
            "extra": "7497 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7497 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 250091,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4500 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 250091,
            "unit": "ns/op",
            "extra": "4500 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4500 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4500 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 34.52,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34598924 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 34.52,
            "unit": "ns/op",
            "extra": "34598924 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34598924 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34598924 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 265.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4460038 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 265.3,
            "unit": "ns/op",
            "extra": "4460038 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4460038 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4460038 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1268,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "922509 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1268,
            "unit": "ns/op",
            "extra": "922509 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "922509 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "922509 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1040,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1040,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 37.55,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32329864 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.55,
            "unit": "ns/op",
            "extra": "32329864 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32329864 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32329864 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.319,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365861271 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.319,
            "unit": "ns/op",
            "extra": "365861271 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365861271 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365861271 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "committer": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "distinct": true,
          "id": "5dfe1b936e24d22258717fade19f80fad4bf678f",
          "message": "docs: strengthen pre-commit checklist in CLAUDE.md\n\nExplicitly require running ALL tests (Go, Rust, Web) and ALL\nbenchmarks (Go, Rust) before every commit. Previous version only\nlisted benchmarks but not individual test suites.\n\nCo-Authored-By: Ivan Volchanskyi <ivan.volchanskyi@gmail.com>",
          "timestamp": "2026-03-01T13:39:03-08:00",
          "tree_id": "b38150240c52292d395629a78989061c1ef42043",
          "url": "https://github.com/volchanskyi/opengate/commit/5dfe1b936e24d22258717fade19f80fad4bf678f"
        },
        "date": 1772401194799,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15125,
            "unit": "ns/op\t    5401 B/op\t      62 allocs/op",
            "extra": "79226 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15125,
            "unit": "ns/op",
            "extra": "79226 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5401,
            "unit": "B/op",
            "extra": "79226 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "79226 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 157213,
            "unit": "ns/op\t   17541 B/op\t     310 allocs/op",
            "extra": "7155 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 157213,
            "unit": "ns/op",
            "extra": "7155 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17541,
            "unit": "B/op",
            "extra": "7155 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7155 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 157718,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7588 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 157718,
            "unit": "ns/op",
            "extra": "7588 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7588 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7588 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 305320,
            "unit": "ns/op\t   28174 B/op\t     409 allocs/op",
            "extra": "3970 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 305320,
            "unit": "ns/op",
            "extra": "3970 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28174,
            "unit": "B/op",
            "extra": "3970 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3970 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 54899,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22174 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 54899,
            "unit": "ns/op",
            "extra": "22174 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22174 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22174 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 366928,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3142 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 366928,
            "unit": "ns/op",
            "extra": "3142 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3142 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3142 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20944,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57428 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20944,
            "unit": "ns/op",
            "extra": "57428 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57428 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57428 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 152282,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6944 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 152282,
            "unit": "ns/op",
            "extra": "6944 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6944 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6944 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 403286,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "2804 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 403286,
            "unit": "ns/op",
            "extra": "2804 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "2804 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "2804 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.69,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34427936 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.69,
            "unit": "ns/op",
            "extra": "34427936 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34427936 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34427936 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 262.4,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4520208 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 262.4,
            "unit": "ns/op",
            "extra": "4520208 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4520208 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4520208 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1209,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "922304 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1209,
            "unit": "ns/op",
            "extra": "922304 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "922304 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "922304 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1085,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1085,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 37.53,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33143072 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.53,
            "unit": "ns/op",
            "extra": "33143072 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33143072 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33143072 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.277,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365768277 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.277,
            "unit": "ns/op",
            "extra": "365768277 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365768277 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365768277 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "committer": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "distinct": true,
          "id": "8cc3eb92de551ecea250ad1eb958840b579861e9",
          "message": "ci: run benchmark jobs after all other CI jobs pass\n\nbench-go and bench-rust now depend on all 11 gate jobs, ensuring\nbenchmarks only run after tests, linting, security audit, and CodeQL\nall succeed.\n\nCo-Authored-By: Ivan Volchanskyi <ivan.volchanskyi@gmail.com>",
          "timestamp": "2026-03-01T13:53:10-08:00",
          "tree_id": "3b4db244ed47882d82f908f224bde1e6e02a11fa",
          "url": "https://github.com/volchanskyi/opengate/commit/8cc3eb92de551ecea250ad1eb958840b579861e9"
        },
        "date": 1772402256177,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15680,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "77148 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15680,
            "unit": "ns/op",
            "extra": "77148 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "77148 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "77148 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 154804,
            "unit": "ns/op\t   17540 B/op\t     309 allocs/op",
            "extra": "7396 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 154804,
            "unit": "ns/op",
            "extra": "7396 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7396 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 309,
            "unit": "allocs/op",
            "extra": "7396 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 155526,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7738 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 155526,
            "unit": "ns/op",
            "extra": "7738 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7738 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7738 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 299990,
            "unit": "ns/op\t   28175 B/op\t     410 allocs/op",
            "extra": "3996 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 299990,
            "unit": "ns/op",
            "extra": "3996 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "3996 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "3996 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 51842,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22927 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 51842,
            "unit": "ns/op",
            "extra": "22927 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22927 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22927 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 263362,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3960 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 263362,
            "unit": "ns/op",
            "extra": "3960 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3960 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3960 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20866,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "58016 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20866,
            "unit": "ns/op",
            "extra": "58016 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "58016 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "58016 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 150823,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7180 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 150823,
            "unit": "ns/op",
            "extra": "7180 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7180 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7180 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 277251,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3951 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 277251,
            "unit": "ns/op",
            "extra": "3951 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3951 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3951 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.93,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34859874 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.93,
            "unit": "ns/op",
            "extra": "34859874 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34859874 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34859874 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 264.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4414548 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 264.3,
            "unit": "ns/op",
            "extra": "4414548 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4414548 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4414548 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1227,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "915639 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1227,
            "unit": "ns/op",
            "extra": "915639 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "915639 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "915639 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1037,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1037,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 37.12,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33414630 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.12,
            "unit": "ns/op",
            "extra": "33414630 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33414630 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33414630 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.285,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "364937679 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.285,
            "unit": "ns/op",
            "extra": "364937679 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "364937679 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "364937679 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "committer": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "distinct": true,
          "id": "b0fc946ca0c66dd09dc76ffb5ed1e34d5b15d6b0",
          "message": "ci: sequence benchmark jobs after auto-merge\n\nChain: merge-to-main → bench-go → bench-rust.\nBenchmarks now run sequentially as the last CI jobs,\nonly after dev has been merged into main.\n\nCo-Authored-By: Ivan Volchanskyi <ivan.volchanskyi@gmail.com>",
          "timestamp": "2026-03-01T14:09:34-08:00",
          "tree_id": "042a138b9dc79d5c7fd1f4905130c78451b4872a",
          "url": "https://github.com/volchanskyi/opengate/commit/b0fc946ca0c66dd09dc76ffb5ed1e34d5b15d6b0"
        },
        "date": 1772403254281,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15672,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "76294 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15672,
            "unit": "ns/op",
            "extra": "76294 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "76294 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "76294 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156052,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7131 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156052,
            "unit": "ns/op",
            "extra": "7131 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7131 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7131 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 157678,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7450 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 157678,
            "unit": "ns/op",
            "extra": "7450 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7450 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7450 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 305089,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "3943 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 305089,
            "unit": "ns/op",
            "extra": "3943 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "3943 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "3943 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 53246,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22620 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 53246,
            "unit": "ns/op",
            "extra": "22620 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22620 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22620 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 325148,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3398 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 325148,
            "unit": "ns/op",
            "extra": "3398 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3398 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3398 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 21166,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56368 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 21166,
            "unit": "ns/op",
            "extra": "56368 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56368 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56368 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 154189,
            "unit": "ns/op\t   41697 B/op\t    1978 allocs/op",
            "extra": "7054 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 154189,
            "unit": "ns/op",
            "extra": "7054 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41697,
            "unit": "B/op",
            "extra": "7054 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7054 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 297409,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3622 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 297409,
            "unit": "ns/op",
            "extra": "3622 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3622 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3622 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.99,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35259303 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.99,
            "unit": "ns/op",
            "extra": "35259303 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35259303 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35259303 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 269.5,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4474300 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 269.5,
            "unit": "ns/op",
            "extra": "4474300 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4474300 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4474300 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1221,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "887414 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1221,
            "unit": "ns/op",
            "extra": "887414 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "887414 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "887414 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1088,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1088,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 38.09,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31714050 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 38.09,
            "unit": "ns/op",
            "extra": "31714050 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31714050 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31714050 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.298,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365172398 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.298,
            "unit": "ns/op",
            "extra": "365172398 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365172398 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365172398 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "committer": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "distinct": true,
          "id": "6fc8dd8666637e28186d663a1cf6c07c9c5c792b",
          "message": "docs: update README with sequential benchmark pipeline\n\nCo-Authored-By: Ivan Volchanskyi <ivan.volchanskyi@gmail.com>",
          "timestamp": "2026-03-01T14:10:18-08:00",
          "tree_id": "70fdf3c5c4501bdd71f7a680496f569d71371e99",
          "url": "https://github.com/volchanskyi/opengate/commit/6fc8dd8666637e28186d663a1cf6c07c9c5c792b"
        },
        "date": 1772403294390,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15470,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "76740 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15470,
            "unit": "ns/op",
            "extra": "76740 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "76740 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "76740 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 155216,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7544 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 155216,
            "unit": "ns/op",
            "extra": "7544 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7544 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7544 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 158693,
            "unit": "ns/op\t   17971 B/op\t     319 allocs/op",
            "extra": "7603 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 158693,
            "unit": "ns/op",
            "extra": "7603 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17971,
            "unit": "B/op",
            "extra": "7603 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7603 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 306877,
            "unit": "ns/op\t   28163 B/op\t     410 allocs/op",
            "extra": "4029 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 306877,
            "unit": "ns/op",
            "extra": "4029 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28163,
            "unit": "B/op",
            "extra": "4029 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4029 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52714,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22880 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52714,
            "unit": "ns/op",
            "extra": "22880 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22880 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22880 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 287331,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "4107 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 287331,
            "unit": "ns/op",
            "extra": "4107 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "4107 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4107 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20928,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56803 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20928,
            "unit": "ns/op",
            "extra": "56803 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56803 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56803 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 150188,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7922 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 150188,
            "unit": "ns/op",
            "extra": "7922 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7922 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7922 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 289047,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3793 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 289047,
            "unit": "ns/op",
            "extra": "3793 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3793 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3793 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.77,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35008209 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.77,
            "unit": "ns/op",
            "extra": "35008209 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35008209 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35008209 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 263.4,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4474243 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 263.4,
            "unit": "ns/op",
            "extra": "4474243 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4474243 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4474243 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1208,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "993129 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1208,
            "unit": "ns/op",
            "extra": "993129 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "993129 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "993129 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1057,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1057,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 36.72,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33172422 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 36.72,
            "unit": "ns/op",
            "extra": "33172422 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33172422 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33172422 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.279,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365796879 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.279,
            "unit": "ns/op",
            "extra": "365796879 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365796879 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365796879 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "committer": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "distinct": true,
          "id": "85d397fdf56f3975b089ede0bf8b4b8f01f4fc2c",
          "message": "ci: trigger CI on PRs to dev, run benchmarks in parallel\n\n- Add dev to pull_request branches so Dependabot PRs get CI checks\n- Decouple bench-go and bench-rust: both need merge-to-main but\n  run independently in parallel\n\nCo-Authored-By: Ivan Volchanskyi <ivan.volchanskyi@gmail.com>",
          "timestamp": "2026-03-01T14:30:44-08:00",
          "tree_id": "afed645cdddf053758ef5e9a76fe904076f3b19f",
          "url": "https://github.com/volchanskyi/opengate/commit/85d397fdf56f3975b089ede0bf8b4b8f01f4fc2c"
        },
        "date": 1772404530958,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15174,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "80307 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15174,
            "unit": "ns/op",
            "extra": "80307 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "80307 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "80307 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156529,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "6722 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156529,
            "unit": "ns/op",
            "extra": "6722 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "6722 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "6722 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 158668,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7700 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 158668,
            "unit": "ns/op",
            "extra": "7700 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7700 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7700 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 304242,
            "unit": "ns/op\t   28161 B/op\t     409 allocs/op",
            "extra": "4052 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 304242,
            "unit": "ns/op",
            "extra": "4052 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28161,
            "unit": "B/op",
            "extra": "4052 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "4052 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 53106,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22640 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 53106,
            "unit": "ns/op",
            "extra": "22640 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22640 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22640 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 366733,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3043 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 366733,
            "unit": "ns/op",
            "extra": "3043 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3043 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3043 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 21145,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56516 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 21145,
            "unit": "ns/op",
            "extra": "56516 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56516 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56516 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 151322,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7293 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 151322,
            "unit": "ns/op",
            "extra": "7293 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7293 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7293 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 400219,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "2830 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 400219,
            "unit": "ns/op",
            "extra": "2830 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "2830 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "2830 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.89,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "33652858 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.89,
            "unit": "ns/op",
            "extra": "33652858 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "33652858 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33652858 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 258.8,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4613391 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 258.8,
            "unit": "ns/op",
            "extra": "4613391 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4613391 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4613391 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1203,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "991216 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1203,
            "unit": "ns/op",
            "extra": "991216 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "991216 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "991216 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1048,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1048,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 36.38,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31905202 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 36.38,
            "unit": "ns/op",
            "extra": "31905202 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31905202 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31905202 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.279,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365276152 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.279,
            "unit": "ns/op",
            "extra": "365276152 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365276152 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365276152 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "committer": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "distinct": true,
          "id": "7a9b08cd2467bad309c403c103eb61010d26df08",
          "message": "docs: update README with branch protection and parallel benchmarks\n\nCo-Authored-By: Ivan Volchanskyi <ivan.volchanskyi@gmail.com>",
          "timestamp": "2026-03-01T14:32:59-08:00",
          "tree_id": "9540aabdc40cd1b0ac8723368e1e96e778ebf5e5",
          "url": "https://github.com/volchanskyi/opengate/commit/7a9b08cd2467bad309c403c103eb61010d26df08"
        },
        "date": 1772404667227,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 14901,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "79088 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 14901,
            "unit": "ns/op",
            "extra": "79088 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "79088 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "79088 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156585,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7606 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156585,
            "unit": "ns/op",
            "extra": "7606 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7606 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7606 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 156203,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7708 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 156203,
            "unit": "ns/op",
            "extra": "7708 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7708 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7708 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 311020,
            "unit": "ns/op\t   28163 B/op\t     410 allocs/op",
            "extra": "3987 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 311020,
            "unit": "ns/op",
            "extra": "3987 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28163,
            "unit": "B/op",
            "extra": "3987 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "3987 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 53269,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22306 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 53269,
            "unit": "ns/op",
            "extra": "22306 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22306 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22306 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 348382,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3230 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 348382,
            "unit": "ns/op",
            "extra": "3230 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3230 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3230 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20864,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57504 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20864,
            "unit": "ns/op",
            "extra": "57504 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57504 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57504 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 151327,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6952 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 151327,
            "unit": "ns/op",
            "extra": "6952 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6952 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6952 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 330213,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3308 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 330213,
            "unit": "ns/op",
            "extra": "3308 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3308 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3308 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.63,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34185426 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.63,
            "unit": "ns/op",
            "extra": "34185426 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34185426 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34185426 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 260.4,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4604442 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 260.4,
            "unit": "ns/op",
            "extra": "4604442 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4604442 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4604442 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1210,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "900645 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1210,
            "unit": "ns/op",
            "extra": "900645 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "900645 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "900645 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1036,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1036,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 36.57,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32639809 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 36.57,
            "unit": "ns/op",
            "extra": "32639809 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32639809 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32639809 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.278,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "366027922 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.278,
            "unit": "ns/op",
            "extra": "366027922 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "366027922 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "366027922 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "committer": {
            "email": "ivan.volchanskyi@gmail.com",
            "name": "volchanskyi",
            "username": "volchanskyi"
          },
          "distinct": true,
          "id": "4aba9799f85796d10079d4af44df29d31d310a5a",
          "message": "ci: run CI on main push for CodeQL, enable Dependabot alerts\n\n- Add main to push trigger so CodeQL scans the default branch\n- Guard merge-to-main with ref check to prevent circular triggers\n- Dependabot vulnerability alerts and security updates enabled via API\n\nCo-Authored-By: Ivan Volchanskyi <ivan.volchanskyi@gmail.com>",
          "timestamp": "2026-03-01T14:38:56-08:00",
          "tree_id": "b0767c59304fb51e1162f24148dcfb27dc04e9d3",
          "url": "https://github.com/volchanskyi/opengate/commit/4aba9799f85796d10079d4af44df29d31d310a5a"
        },
        "date": 1772405021626,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15226,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "78982 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15226,
            "unit": "ns/op",
            "extra": "78982 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "78982 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "78982 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156897,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7369 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156897,
            "unit": "ns/op",
            "extra": "7369 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7369 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7369 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 157613,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7521 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 157613,
            "unit": "ns/op",
            "extra": "7521 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7521 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7521 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 311350,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "3879 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 311350,
            "unit": "ns/op",
            "extra": "3879 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "3879 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "3879 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 53966,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22338 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 53966,
            "unit": "ns/op",
            "extra": "22338 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22338 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22338 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 403878,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3013 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 403878,
            "unit": "ns/op",
            "extra": "3013 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3013 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3013 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20969,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56103 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20969,
            "unit": "ns/op",
            "extra": "56103 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56103 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56103 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 164071,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7294 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 164071,
            "unit": "ns/op",
            "extra": "7294 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7294 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7294 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 455759,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "2222 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 455759,
            "unit": "ns/op",
            "extra": "2222 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "2222 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "2222 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 34.88,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34104390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 34.88,
            "unit": "ns/op",
            "extra": "34104390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34104390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34104390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 265.6,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4531228 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 265.6,
            "unit": "ns/op",
            "extra": "4531228 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4531228 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4531228 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1204,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1204,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1071,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1071,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 36.94,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32880680 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 36.94,
            "unit": "ns/op",
            "extra": "32880680 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32880680 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32880680 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.276,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365946477 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.276,
            "unit": "ns/op",
            "extra": "365946477 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365946477 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365946477 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "committer": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "id": "e2c3b7b7da29097ff480c1ec48e3021a6e4745e5",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T22:52:39Z",
          "url": "https://github.com/volchanskyi/opengate/commit/e2c3b7b7da29097ff480c1ec48e3021a6e4745e5"
        },
        "date": 1772405625247,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 16426,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "74437 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 16426,
            "unit": "ns/op",
            "extra": "74437 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "74437 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "74437 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156013,
            "unit": "ns/op\t   17540 B/op\t     309 allocs/op",
            "extra": "7460 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156013,
            "unit": "ns/op",
            "extra": "7460 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7460 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 309,
            "unit": "allocs/op",
            "extra": "7460 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 160120,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 160120,
            "unit": "ns/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 306851,
            "unit": "ns/op\t   28175 B/op\t     409 allocs/op",
            "extra": "3820 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 306851,
            "unit": "ns/op",
            "extra": "3820 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "3820 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3820 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 53547,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22600 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 53547,
            "unit": "ns/op",
            "extra": "22600 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22600 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22600 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 280846,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4318 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 280846,
            "unit": "ns/op",
            "extra": "4318 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4318 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4318 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20905,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56676 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20905,
            "unit": "ns/op",
            "extra": "56676 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56676 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56676 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 155588,
            "unit": "ns/op\t   41697 B/op\t    1978 allocs/op",
            "extra": "7501 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 155588,
            "unit": "ns/op",
            "extra": "7501 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41697,
            "unit": "B/op",
            "extra": "7501 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7501 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 296115,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3704 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 296115,
            "unit": "ns/op",
            "extra": "3704 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3704 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3704 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.87,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35249718 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.87,
            "unit": "ns/op",
            "extra": "35249718 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35249718 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35249718 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 263.8,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4596194 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 263.8,
            "unit": "ns/op",
            "extra": "4596194 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4596194 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4596194 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1212,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "916868 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1212,
            "unit": "ns/op",
            "extra": "916868 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "916868 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "916868 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1061,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1061,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 37.14,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32307872 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.14,
            "unit": "ns/op",
            "extra": "32307872 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32307872 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32307872 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.302,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "366160426 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.302,
            "unit": "ns/op",
            "extra": "366160426 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "366160426 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "366160426 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "committer": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "id": "9d1bcd488062b009c1942b34997b4654f6bfbc64",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T22:57:10Z",
          "url": "https://github.com/volchanskyi/opengate/commit/9d1bcd488062b009c1942b34997b4654f6bfbc64"
        },
        "date": 1772405872647,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 14597,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "84636 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 14597,
            "unit": "ns/op",
            "extra": "84636 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "84636 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "84636 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 155883,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7024 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 155883,
            "unit": "ns/op",
            "extra": "7024 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7024 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7024 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 156138,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7442 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 156138,
            "unit": "ns/op",
            "extra": "7442 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7442 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7442 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 230960,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "5128 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 230960,
            "unit": "ns/op",
            "extra": "5128 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "5128 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "5128 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 35842,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "33313 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 35842,
            "unit": "ns/op",
            "extra": "33313 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "33313 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "33313 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 179280,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "6841 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 179280,
            "unit": "ns/op",
            "extra": "6841 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "6841 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "6841 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 17040,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "69834 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 17040,
            "unit": "ns/op",
            "extra": "69834 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "69834 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "69834 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 165696,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6734 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 165696,
            "unit": "ns/op",
            "extra": "6734 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6734 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6734 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 166782,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "6112 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 166782,
            "unit": "ns/op",
            "extra": "6112 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "6112 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "6112 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 32.12,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "37325228 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 32.12,
            "unit": "ns/op",
            "extra": "37325228 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "37325228 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "37325228 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 275.9,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4324698 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 275.9,
            "unit": "ns/op",
            "extra": "4324698 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4324698 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4324698 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1175,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "971475 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1175,
            "unit": "ns/op",
            "extra": "971475 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "971475 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "971475 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1020,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1020,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 35.28,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "34413522 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 35.28,
            "unit": "ns/op",
            "extra": "34413522 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "34413522 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34413522 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 2.885,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "415350103 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 2.885,
            "unit": "ns/op",
            "extra": "415350103 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "415350103 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "415350103 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "committer": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "id": "c549281fd02679b4f87c7fa5eba75a41fcc667a7",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T22:59:21Z",
          "url": "https://github.com/volchanskyi/opengate/commit/c549281fd02679b4f87c7fa5eba75a41fcc667a7"
        },
        "date": 1772406006620,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15067,
            "unit": "ns/op\t    5401 B/op\t      62 allocs/op",
            "extra": "78956 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15067,
            "unit": "ns/op",
            "extra": "78956 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5401,
            "unit": "B/op",
            "extra": "78956 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "78956 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 167295,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "6836 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 167295,
            "unit": "ns/op",
            "extra": "6836 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "6836 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "6836 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 162781,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7514 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 162781,
            "unit": "ns/op",
            "extra": "7514 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7514 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7514 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 358942,
            "unit": "ns/op\t   28175 B/op\t     409 allocs/op",
            "extra": "3987 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 358942,
            "unit": "ns/op",
            "extra": "3987 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "3987 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3987 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 53316,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22508 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 53316,
            "unit": "ns/op",
            "extra": "22508 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22508 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22508 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 408368,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "2996 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 408368,
            "unit": "ns/op",
            "extra": "2996 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "2996 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "2996 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20957,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56977 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20957,
            "unit": "ns/op",
            "extra": "56977 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56977 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56977 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 151343,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6907 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 151343,
            "unit": "ns/op",
            "extra": "6907 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6907 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6907 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 431049,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "2425 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 431049,
            "unit": "ns/op",
            "extra": "2425 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "2425 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "2425 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.81,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34875462 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.81,
            "unit": "ns/op",
            "extra": "34875462 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34875462 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34875462 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 262.7,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4498111 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 262.7,
            "unit": "ns/op",
            "extra": "4498111 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4498111 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4498111 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1227,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "971258 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1227,
            "unit": "ns/op",
            "extra": "971258 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "971258 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "971258 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1057,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1057,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 37.01,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32691637 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.01,
            "unit": "ns/op",
            "extra": "32691637 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32691637 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32691637 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.28,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365979312 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.28,
            "unit": "ns/op",
            "extra": "365979312 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365979312 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365979312 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "committer": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "id": "c549281fd02679b4f87c7fa5eba75a41fcc667a7",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T22:59:21Z",
          "url": "https://github.com/volchanskyi/opengate/commit/c549281fd02679b4f87c7fa5eba75a41fcc667a7"
        },
        "date": 1772406116292,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15824,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "75470 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15824,
            "unit": "ns/op",
            "extra": "75470 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "75470 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "75470 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156469,
            "unit": "ns/op\t   17539 B/op\t     309 allocs/op",
            "extra": "7572 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156469,
            "unit": "ns/op",
            "extra": "7572 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17539,
            "unit": "B/op",
            "extra": "7572 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 309,
            "unit": "allocs/op",
            "extra": "7572 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 157125,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7526 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 157125,
            "unit": "ns/op",
            "extra": "7526 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7526 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7526 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 305561,
            "unit": "ns/op\t   28175 B/op\t     410 allocs/op",
            "extra": "4003 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 305561,
            "unit": "ns/op",
            "extra": "4003 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "4003 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4003 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 53899,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22554 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 53899,
            "unit": "ns/op",
            "extra": "22554 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22554 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22554 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 308890,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3982 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 308890,
            "unit": "ns/op",
            "extra": "3982 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3982 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3982 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20732,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "58140 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20732,
            "unit": "ns/op",
            "extra": "58140 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "58140 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "58140 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 150057,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7208 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 150057,
            "unit": "ns/op",
            "extra": "7208 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7208 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7208 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 276443,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4036 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 276443,
            "unit": "ns/op",
            "extra": "4036 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4036 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4036 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 34.57,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34620196 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 34.57,
            "unit": "ns/op",
            "extra": "34620196 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34620196 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34620196 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 262.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4624933 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 262.3,
            "unit": "ns/op",
            "extra": "4624933 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4624933 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4624933 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1209,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "901170 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1209,
            "unit": "ns/op",
            "extra": "901170 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "901170 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "901170 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1044,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1044,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 37.69,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31910220 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.69,
            "unit": "ns/op",
            "extra": "31910220 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31910220 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31910220 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.302,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "356450392 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.302,
            "unit": "ns/op",
            "extra": "356450392 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "356450392 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "356450392 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "committer": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "id": "172e3e7406114567c7f3d8649f4b4e7d66901ac8",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T23:05:41Z",
          "url": "https://github.com/volchanskyi/opengate/commit/172e3e7406114567c7f3d8649f4b4e7d66901ac8"
        },
        "date": 1772406379792,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 14760,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "82040 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 14760,
            "unit": "ns/op",
            "extra": "82040 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "82040 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "82040 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 154902,
            "unit": "ns/op\t   17540 B/op\t     309 allocs/op",
            "extra": "7639 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 154902,
            "unit": "ns/op",
            "extra": "7639 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7639 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 309,
            "unit": "allocs/op",
            "extra": "7639 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 155363,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7737 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 155363,
            "unit": "ns/op",
            "extra": "7737 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7737 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7737 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 300401,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "4039 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 300401,
            "unit": "ns/op",
            "extra": "4039 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "4039 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4039 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52524,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22856 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52524,
            "unit": "ns/op",
            "extra": "22856 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22856 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22856 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 275342,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4650 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 275342,
            "unit": "ns/op",
            "extra": "4650 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4650 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4650 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20121,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "58689 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20121,
            "unit": "ns/op",
            "extra": "58689 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "58689 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "58689 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 149650,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7658 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 149650,
            "unit": "ns/op",
            "extra": "7658 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7658 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7658 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 254278,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4269 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 254278,
            "unit": "ns/op",
            "extra": "4269 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4269 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4269 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.36,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35563549 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.36,
            "unit": "ns/op",
            "extra": "35563549 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35563549 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35563549 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 260.4,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4506921 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 260.4,
            "unit": "ns/op",
            "extra": "4506921 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4506921 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4506921 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1214,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "875062 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1214,
            "unit": "ns/op",
            "extra": "875062 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "875062 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "875062 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1069,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1069,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 36.65,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33035320 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 36.65,
            "unit": "ns/op",
            "extra": "33035320 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33035320 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33035320 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.277,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365549084 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.277,
            "unit": "ns/op",
            "extra": "365549084 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365549084 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365549084 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "committer": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "id": "acbafb20128a15550f800db9b032c5c0500dcced",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T23:24:31Z",
          "url": "https://github.com/volchanskyi/opengate/commit/acbafb20128a15550f800db9b032c5c0500dcced"
        },
        "date": 1772407511080,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 14812,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "81496 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 14812,
            "unit": "ns/op",
            "extra": "81496 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "81496 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "81496 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 155609,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7515 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 155609,
            "unit": "ns/op",
            "extra": "7515 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7515 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7515 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 155345,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7472 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 155345,
            "unit": "ns/op",
            "extra": "7472 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7472 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7472 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 300569,
            "unit": "ns/op\t   28176 B/op\t     409 allocs/op",
            "extra": "4120 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 300569,
            "unit": "ns/op",
            "extra": "4120 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "4120 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "4120 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52097,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22842 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52097,
            "unit": "ns/op",
            "extra": "22842 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22842 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22842 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 327647,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3886 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 327647,
            "unit": "ns/op",
            "extra": "3886 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3886 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3886 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20689,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57876 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20689,
            "unit": "ns/op",
            "extra": "57876 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57876 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57876 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 154431,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6950 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 154431,
            "unit": "ns/op",
            "extra": "6950 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6950 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6950 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 320855,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3373 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 320855,
            "unit": "ns/op",
            "extra": "3373 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3373 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3373 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.56,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35659041 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.56,
            "unit": "ns/op",
            "extra": "35659041 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35659041 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35659041 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 258.1,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4671882 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 258.1,
            "unit": "ns/op",
            "extra": "4671882 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4671882 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4671882 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1208,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "953578 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1208,
            "unit": "ns/op",
            "extra": "953578 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "953578 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "953578 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1041,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1041,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 36.69,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33086862 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 36.69,
            "unit": "ns/op",
            "extra": "33086862 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33086862 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33086862 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.28,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "366074242 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.28,
            "unit": "ns/op",
            "extra": "366074242 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "366074242 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "366074242 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "committer": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "id": "815f2f9e42a9aab416f70f1051ae39224068d936",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T23:34:12Z",
          "url": "https://github.com/volchanskyi/opengate/commit/815f2f9e42a9aab416f70f1051ae39224068d936"
        },
        "date": 1772408094450,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15686,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "76720 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15686,
            "unit": "ns/op",
            "extra": "76720 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "76720 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "76720 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156575,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7083 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156575,
            "unit": "ns/op",
            "extra": "7083 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7083 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7083 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 156798,
            "unit": "ns/op\t   17973 B/op\t     319 allocs/op",
            "extra": "7464 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 156798,
            "unit": "ns/op",
            "extra": "7464 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17973,
            "unit": "B/op",
            "extra": "7464 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7464 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 306116,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "3955 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 306116,
            "unit": "ns/op",
            "extra": "3955 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "3955 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "3955 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52795,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22554 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52795,
            "unit": "ns/op",
            "extra": "22554 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22554 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22554 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 291118,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "4063 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 291118,
            "unit": "ns/op",
            "extra": "4063 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "4063 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4063 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20615,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57656 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20615,
            "unit": "ns/op",
            "extra": "57656 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57656 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57656 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 152373,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7872 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 152373,
            "unit": "ns/op",
            "extra": "7872 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7872 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7872 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 271653,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4134 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 271653,
            "unit": "ns/op",
            "extra": "4134 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4134 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4134 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 34.82,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34458824 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 34.82,
            "unit": "ns/op",
            "extra": "34458824 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34458824 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34458824 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 269.2,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4495288 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 269.2,
            "unit": "ns/op",
            "extra": "4495288 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4495288 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4495288 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1207,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "913312 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1207,
            "unit": "ns/op",
            "extra": "913312 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "913312 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "913312 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1038,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1038,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 37.78,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31380820 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.78,
            "unit": "ns/op",
            "extra": "31380820 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31380820 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31380820 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.281,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365870505 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.281,
            "unit": "ns/op",
            "extra": "365870505 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365870505 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365870505 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "committer": {
            "name": "github-actions[bot]",
            "username": "github-actions[bot]",
            "email": "github-actions[bot]@users.noreply.github.com"
          },
          "id": "6f9ea25dff896fd6c51ea3655adc00aa5f41deba",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T23:45:07Z",
          "url": "https://github.com/volchanskyi/opengate/commit/6f9ea25dff896fd6c51ea3655adc00aa5f41deba"
        },
        "date": 1772408750530,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15936,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "76857 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15936,
            "unit": "ns/op",
            "extra": "76857 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "76857 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "76857 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 160650,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7200 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 160650,
            "unit": "ns/op",
            "extra": "7200 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7200 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7200 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 161243,
            "unit": "ns/op\t   17973 B/op\t     319 allocs/op",
            "extra": "7630 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 161243,
            "unit": "ns/op",
            "extra": "7630 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17973,
            "unit": "B/op",
            "extra": "7630 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7630 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 243170,
            "unit": "ns/op\t   28175 B/op\t     410 allocs/op",
            "extra": "4888 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 243170,
            "unit": "ns/op",
            "extra": "4888 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "4888 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4888 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 37902,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "31652 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 37902,
            "unit": "ns/op",
            "extra": "31652 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "31652 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "31652 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 271858,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "5014 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 271858,
            "unit": "ns/op",
            "extra": "5014 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "5014 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "5014 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 17681,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "65763 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 17681,
            "unit": "ns/op",
            "extra": "65763 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "65763 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "65763 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 173223,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6690 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 173223,
            "unit": "ns/op",
            "extra": "6690 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6690 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6690 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 320896,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3338 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 320896,
            "unit": "ns/op",
            "extra": "3338 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3338 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3338 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 32.72,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "36471876 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 32.72,
            "unit": "ns/op",
            "extra": "36471876 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "36471876 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "36471876 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 272.9,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4366269 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 272.9,
            "unit": "ns/op",
            "extra": "4366269 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4366269 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4366269 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1221,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "992881 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1221,
            "unit": "ns/op",
            "extra": "992881 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "992881 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "992881 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1071,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1071,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 35.81,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33495642 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 35.81,
            "unit": "ns/op",
            "extra": "33495642 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33495642 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33495642 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 2.902,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "412415890 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 2.902,
            "unit": "ns/op",
            "extra": "412415890 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "412415890 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "412415890 times\n4 procs"
          }
        ]
      }
    ]
  }
}