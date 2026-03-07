window.BENCHMARK_DATA = {
  "lastUpdate": 1772876856536,
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
        "date": 1772408780360,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 16362,
            "unit": "ns/op\t    5417 B/op\t      62 allocs/op",
            "extra": "64783 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 16362,
            "unit": "ns/op",
            "extra": "64783 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5417,
            "unit": "B/op",
            "extra": "64783 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "64783 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 158028,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7422 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 158028,
            "unit": "ns/op",
            "extra": "7422 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7422 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7422 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 158336,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7156 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 158336,
            "unit": "ns/op",
            "extra": "7156 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7156 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7156 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 308233,
            "unit": "ns/op\t   28175 B/op\t     409 allocs/op",
            "extra": "3999 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 308233,
            "unit": "ns/op",
            "extra": "3999 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "3999 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3999 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52071,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22860 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52071,
            "unit": "ns/op",
            "extra": "22860 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22860 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22860 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 293894,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3637 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 293894,
            "unit": "ns/op",
            "extra": "3637 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3637 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3637 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20672,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "58071 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20672,
            "unit": "ns/op",
            "extra": "58071 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "58071 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "58071 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 151322,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7020 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 151322,
            "unit": "ns/op",
            "extra": "7020 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7020 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7020 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 280221,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3968 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 280221,
            "unit": "ns/op",
            "extra": "3968 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3968 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3968 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 34.13,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34778481 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 34.13,
            "unit": "ns/op",
            "extra": "34778481 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34778481 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34778481 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 271.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4427798 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 271.3,
            "unit": "ns/op",
            "extra": "4427798 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4427798 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4427798 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1224,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "968589 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1224,
            "unit": "ns/op",
            "extra": "968589 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "968589 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "968589 times\n4 procs"
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
            "value": 37.52,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31254880 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.52,
            "unit": "ns/op",
            "extra": "31254880 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31254880 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31254880 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.281,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "364680770 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.281,
            "unit": "ns/op",
            "extra": "364680770 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "364680770 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "364680770 times\n4 procs"
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
        "date": 1772408888913,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 14431,
            "unit": "ns/op\t    5401 B/op\t      62 allocs/op",
            "extra": "83295 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 14431,
            "unit": "ns/op",
            "extra": "83295 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5401,
            "unit": "B/op",
            "extra": "83295 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "83295 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 155820,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7519 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 155820,
            "unit": "ns/op",
            "extra": "7519 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7519 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7519 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 157067,
            "unit": "ns/op\t   17973 B/op\t     319 allocs/op",
            "extra": "7668 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 157067,
            "unit": "ns/op",
            "extra": "7668 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17973,
            "unit": "B/op",
            "extra": "7668 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7668 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 233979,
            "unit": "ns/op\t   28161 B/op\t     409 allocs/op",
            "extra": "5125 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 233979,
            "unit": "ns/op",
            "extra": "5125 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28161,
            "unit": "B/op",
            "extra": "5125 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "5125 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 36141,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "33027 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 36141,
            "unit": "ns/op",
            "extra": "33027 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "33027 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "33027 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 192406,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "6535 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 192406,
            "unit": "ns/op",
            "extra": "6535 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "6535 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "6535 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 17115,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "69561 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 17115,
            "unit": "ns/op",
            "extra": "69561 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "69561 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "69561 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 166745,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6546 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 166745,
            "unit": "ns/op",
            "extra": "6546 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6546 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6546 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 168423,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "6453 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 168423,
            "unit": "ns/op",
            "extra": "6453 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "6453 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "6453 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 32.01,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "36600758 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 32.01,
            "unit": "ns/op",
            "extra": "36600758 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "36600758 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "36600758 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 282.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4271706 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 282.3,
            "unit": "ns/op",
            "extra": "4271706 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4271706 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4271706 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1188,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "964939 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1188,
            "unit": "ns/op",
            "extra": "964939 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "964939 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "964939 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1023,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1023,
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
            "value": 35.1,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "34747532 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 35.1,
            "unit": "ns/op",
            "extra": "34747532 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "34747532 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34747532 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 2.889,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "413844858 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 2.889,
            "unit": "ns/op",
            "extra": "413844858 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "413844858 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "413844858 times\n4 procs"
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
          "id": "f14387204069f60b2eb8ff386fe15cffbbf36af2",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-01T23:57:14Z",
          "url": "https://github.com/volchanskyi/opengate/commit/f14387204069f60b2eb8ff386fe15cffbbf36af2"
        },
        "date": 1772409473198,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 14886,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "80034 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 14886,
            "unit": "ns/op",
            "extra": "80034 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "80034 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "80034 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 155114,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7597 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 155114,
            "unit": "ns/op",
            "extra": "7597 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7597 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7597 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 155815,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7221 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 155815,
            "unit": "ns/op",
            "extra": "7221 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7221 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7221 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 303292,
            "unit": "ns/op\t   28175 B/op\t     410 allocs/op",
            "extra": "4124 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 303292,
            "unit": "ns/op",
            "extra": "4124 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "4124 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4124 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52558,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "23034 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52558,
            "unit": "ns/op",
            "extra": "23034 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "23034 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "23034 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 369340,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3344 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 369340,
            "unit": "ns/op",
            "extra": "3344 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3344 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3344 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20761,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57921 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20761,
            "unit": "ns/op",
            "extra": "57921 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57921 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57921 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 150578,
            "unit": "ns/op\t   41697 B/op\t    1978 allocs/op",
            "extra": "7160 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 150578,
            "unit": "ns/op",
            "extra": "7160 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41697,
            "unit": "B/op",
            "extra": "7160 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7160 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 363388,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3012 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 363388,
            "unit": "ns/op",
            "extra": "3012 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3012 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3012 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.83,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "33573674 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.83,
            "unit": "ns/op",
            "extra": "33573674 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "33573674 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33573674 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 259.4,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4644859 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 259.4,
            "unit": "ns/op",
            "extra": "4644859 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4644859 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4644859 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1206,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "934843 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1206,
            "unit": "ns/op",
            "extra": "934843 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "934843 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "934843 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1078,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1078,
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
            "value": 36.52,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32393992 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 36.52,
            "unit": "ns/op",
            "extra": "32393992 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32393992 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32393992 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.276,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "366452372 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.276,
            "unit": "ns/op",
            "extra": "366452372 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "366452372 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "366452372 times\n4 procs"
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
          "id": "a12d1e725fe458c8ac4d6fdb497166a68f0bfd43",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-02T00:07:10Z",
          "url": "https://github.com/volchanskyi/opengate/commit/a12d1e725fe458c8ac4d6fdb497166a68f0bfd43"
        },
        "date": 1772410071600,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15598,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "76359 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15598,
            "unit": "ns/op",
            "extra": "76359 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "76359 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "76359 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 159400,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7515 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 159400,
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
            "value": 159104,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7534 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 159104,
            "unit": "ns/op",
            "extra": "7534 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7534 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7534 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 301228,
            "unit": "ns/op\t   28174 B/op\t     409 allocs/op",
            "extra": "3834 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 301228,
            "unit": "ns/op",
            "extra": "3834 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28174,
            "unit": "B/op",
            "extra": "3834 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3834 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52265,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22011 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52265,
            "unit": "ns/op",
            "extra": "22011 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22011 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22011 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 296639,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3414 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 296639,
            "unit": "ns/op",
            "extra": "3414 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3414 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3414 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20407,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "58224 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20407,
            "unit": "ns/op",
            "extra": "58224 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "58224 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "58224 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 150598,
            "unit": "ns/op\t   41697 B/op\t    1978 allocs/op",
            "extra": "6932 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 150598,
            "unit": "ns/op",
            "extra": "6932 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41697,
            "unit": "B/op",
            "extra": "6932 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6932 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 297848,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3745 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 297848,
            "unit": "ns/op",
            "extra": "3745 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3745 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3745 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 34.06,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "32992383 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 34.06,
            "unit": "ns/op",
            "extra": "32992383 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "32992383 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32992383 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 266.5,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4609070 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 266.5,
            "unit": "ns/op",
            "extra": "4609070 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4609070 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4609070 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1205,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "913801 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1205,
            "unit": "ns/op",
            "extra": "913801 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "913801 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "913801 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1093,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "998726 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1093,
            "unit": "ns/op",
            "extra": "998726 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "998726 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "998726 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 37.03,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32394864 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.03,
            "unit": "ns/op",
            "extra": "32394864 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32394864 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32394864 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.295,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "360257034 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.295,
            "unit": "ns/op",
            "extra": "360257034 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "360257034 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "360257034 times\n4 procs"
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
          "id": "9ed07dc5edf7d6a94ed1c756fdecb724973454e7",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-02T00:22:44Z",
          "url": "https://github.com/volchanskyi/opengate/commit/9ed07dc5edf7d6a94ed1c756fdecb724973454e7"
        },
        "date": 1772411009969,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15552,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "77054 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15552,
            "unit": "ns/op",
            "extra": "77054 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "77054 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "77054 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 158246,
            "unit": "ns/op\t   17539 B/op\t     309 allocs/op",
            "extra": "7623 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 158246,
            "unit": "ns/op",
            "extra": "7623 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17539,
            "unit": "B/op",
            "extra": "7623 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 309,
            "unit": "allocs/op",
            "extra": "7623 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 160462,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7416 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 160462,
            "unit": "ns/op",
            "extra": "7416 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7416 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7416 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 308340,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "4026 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 308340,
            "unit": "ns/op",
            "extra": "4026 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "4026 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4026 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52513,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22824 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52513,
            "unit": "ns/op",
            "extra": "22824 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22824 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22824 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 285345,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4311 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 285345,
            "unit": "ns/op",
            "extra": "4311 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4311 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4311 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20496,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57816 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20496,
            "unit": "ns/op",
            "extra": "57816 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57816 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57816 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 150361,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7045 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 150361,
            "unit": "ns/op",
            "extra": "7045 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7045 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7045 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 261938,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4159 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 261938,
            "unit": "ns/op",
            "extra": "4159 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4159 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4159 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.66,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34779740 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.66,
            "unit": "ns/op",
            "extra": "34779740 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34779740 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34779740 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 264.1,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4518632 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 264.1,
            "unit": "ns/op",
            "extra": "4518632 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4518632 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4518632 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1209,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "998556 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1209,
            "unit": "ns/op",
            "extra": "998556 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "998556 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "998556 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1039,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1039,
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
            "value": 37.08,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32133511 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.08,
            "unit": "ns/op",
            "extra": "32133511 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32133511 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32133511 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.277,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365950340 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.277,
            "unit": "ns/op",
            "extra": "365950340 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365950340 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365950340 times\n4 procs"
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
          "id": "eae3f572614deed6d86aedc248f16615079b9ddd",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-02T00:56:36Z",
          "url": "https://github.com/volchanskyi/opengate/commit/eae3f572614deed6d86aedc248f16615079b9ddd"
        },
        "date": 1772413039491,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 16249,
            "unit": "ns/op\t    5401 B/op\t      62 allocs/op",
            "extra": "73576 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 16249,
            "unit": "ns/op",
            "extra": "73576 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5401,
            "unit": "B/op",
            "extra": "73576 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "73576 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 162204,
            "unit": "ns/op\t   17541 B/op\t     310 allocs/op",
            "extra": "7478 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 162204,
            "unit": "ns/op",
            "extra": "7478 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17541,
            "unit": "B/op",
            "extra": "7478 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7478 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 158379,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 158379,
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
            "value": 312782,
            "unit": "ns/op\t   28174 B/op\t     410 allocs/op",
            "extra": "3879 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 312782,
            "unit": "ns/op",
            "extra": "3879 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28174,
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
            "value": 53150,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22444 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 53150,
            "unit": "ns/op",
            "extra": "22444 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22444 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22444 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 278997,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "4150 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 278997,
            "unit": "ns/op",
            "extra": "4150 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "4150 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4150 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 21259,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57230 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 21259,
            "unit": "ns/op",
            "extra": "57230 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57230 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57230 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 156571,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6669 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 156571,
            "unit": "ns/op",
            "extra": "6669 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6669 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6669 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 294731,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3674 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 294731,
            "unit": "ns/op",
            "extra": "3674 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3674 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3674 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.92,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "33859422 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.92,
            "unit": "ns/op",
            "extra": "33859422 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "33859422 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33859422 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 268.8,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4580272 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 268.8,
            "unit": "ns/op",
            "extra": "4580272 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4580272 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4580272 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1216,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "969226 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1216,
            "unit": "ns/op",
            "extra": "969226 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "969226 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "969226 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1058,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1058,
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
            "value": 37.31,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "30034770 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.31,
            "unit": "ns/op",
            "extra": "30034770 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "30034770 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "30034770 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.346,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "362919368 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.346,
            "unit": "ns/op",
            "extra": "362919368 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "362919368 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "362919368 times\n4 procs"
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
          "id": "e1d866054721a6b3f53504678a5b150a3680e6ad",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-02T01:18:13Z",
          "url": "https://github.com/volchanskyi/opengate/commit/e1d866054721a6b3f53504678a5b150a3680e6ad"
        },
        "date": 1772414333657,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 14444,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "83721 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 14444,
            "unit": "ns/op",
            "extra": "83721 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "83721 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "83721 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 170094,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7159 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 170094,
            "unit": "ns/op",
            "extra": "7159 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7159 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7159 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 170675,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7095 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 170675,
            "unit": "ns/op",
            "extra": "7095 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7095 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7095 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 307127,
            "unit": "ns/op\t   28162 B/op\t     410 allocs/op",
            "extra": "3967 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 307127,
            "unit": "ns/op",
            "extra": "3967 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28162,
            "unit": "B/op",
            "extra": "3967 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "3967 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 59008,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "20509 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 59008,
            "unit": "ns/op",
            "extra": "20509 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "20509 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "20509 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 212689,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4808 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 212689,
            "unit": "ns/op",
            "extra": "4808 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4808 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4808 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 17868,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "66254 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 17868,
            "unit": "ns/op",
            "extra": "66254 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "66254 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "66254 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 133520,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "8481 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 133520,
            "unit": "ns/op",
            "extra": "8481 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "8481 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "8481 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 214922,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "5056 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 214922,
            "unit": "ns/op",
            "extra": "5056 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "5056 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "5056 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 33.56,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35433566 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 33.56,
            "unit": "ns/op",
            "extra": "35433566 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35433566 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35433566 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 266.5,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4487632 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 266.5,
            "unit": "ns/op",
            "extra": "4487632 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4487632 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4487632 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1111,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1111,
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
            "value": 960.3,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1302390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 960.3,
            "unit": "ns/op",
            "extra": "1302390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1302390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1302390 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello",
            "value": 35.17,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33360480 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 35.17,
            "unit": "ns/op",
            "extra": "33360480 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33360480 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33360480 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.544,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "338698509 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.544,
            "unit": "ns/op",
            "extra": "338698509 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "338698509 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "338698509 times\n4 procs"
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
          "id": "d1f810d828a166b520c77929e9c75101a1904f29",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-02T06:37:41Z",
          "url": "https://github.com/volchanskyi/opengate/commit/d1f810d828a166b520c77929e9c75101a1904f29"
        },
        "date": 1772433505672,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15239,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "80455 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15239,
            "unit": "ns/op",
            "extra": "80455 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "80455 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "80455 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 156590,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7494 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 156590,
            "unit": "ns/op",
            "extra": "7494 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7494 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7494 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 156961,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7602 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 156961,
            "unit": "ns/op",
            "extra": "7602 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7602 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7602 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 233212,
            "unit": "ns/op\t   28175 B/op\t     409 allocs/op",
            "extra": "5103 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 233212,
            "unit": "ns/op",
            "extra": "5103 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "5103 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "5103 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 36519,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "32620 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 36519,
            "unit": "ns/op",
            "extra": "32620 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "32620 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "32620 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 182673,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "6801 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 182673,
            "unit": "ns/op",
            "extra": "6801 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "6801 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "6801 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 17184,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "69354 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 17184,
            "unit": "ns/op",
            "extra": "69354 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "69354 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "69354 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 168332,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6654 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 168332,
            "unit": "ns/op",
            "extra": "6654 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6654 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6654 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 157521,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "6766 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 157521,
            "unit": "ns/op",
            "extra": "6766 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "6766 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "6766 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 31.95,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35568248 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 31.95,
            "unit": "ns/op",
            "extra": "35568248 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35568248 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35568248 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 273.6,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4404622 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 273.6,
            "unit": "ns/op",
            "extra": "4404622 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4404622 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4404622 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1168,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1168,
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
            "value": 1027,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1027,
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
            "value": 36,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33103545 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 36,
            "unit": "ns/op",
            "extra": "33103545 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33103545 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33103545 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 2.893,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "415550280 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 2.893,
            "unit": "ns/op",
            "extra": "415550280 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "415550280 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "415550280 times\n4 procs"
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
          "id": "d1f810d828a166b520c77929e9c75101a1904f29",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-02T06:37:41Z",
          "url": "https://github.com/volchanskyi/opengate/commit/d1f810d828a166b520c77929e9c75101a1904f29"
        },
        "date": 1772433610442,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 14533,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "83160 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 14533,
            "unit": "ns/op",
            "extra": "83160 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "83160 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "83160 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 157356,
            "unit": "ns/op\t   17540 B/op\t     309 allocs/op",
            "extra": "7614 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 157356,
            "unit": "ns/op",
            "extra": "7614 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7614 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 309,
            "unit": "allocs/op",
            "extra": "7614 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 156577,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7677 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 156577,
            "unit": "ns/op",
            "extra": "7677 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7677 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7677 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 232494,
            "unit": "ns/op\t   28174 B/op\t     410 allocs/op",
            "extra": "5168 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 232494,
            "unit": "ns/op",
            "extra": "5168 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28174,
            "unit": "B/op",
            "extra": "5168 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "5168 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 36266,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "32602 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 36266,
            "unit": "ns/op",
            "extra": "32602 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "32602 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "32602 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 191048,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "6669 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 191048,
            "unit": "ns/op",
            "extra": "6669 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "6669 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "6669 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 17133,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "69663 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 17133,
            "unit": "ns/op",
            "extra": "69663 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "69663 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "69663 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 167014,
            "unit": "ns/op\t   41697 B/op\t    1978 allocs/op",
            "extra": "6752 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 167014,
            "unit": "ns/op",
            "extra": "6752 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41697,
            "unit": "B/op",
            "extra": "6752 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6752 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 169624,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "6297 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 169624,
            "unit": "ns/op",
            "extra": "6297 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "6297 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "6297 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 41.13,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "28703059 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 41.13,
            "unit": "ns/op",
            "extra": "28703059 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "28703059 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "28703059 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 273.1,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4423083 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 273.1,
            "unit": "ns/op",
            "extra": "4423083 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4423083 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4423083 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1169,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "972967 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1169,
            "unit": "ns/op",
            "extra": "972967 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "972967 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "972967 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1018,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1018,
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
            "value": 35.01,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "34164897 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 35.01,
            "unit": "ns/op",
            "extra": "34164897 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "34164897 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34164897 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 2.893,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "411116907 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 2.893,
            "unit": "ns/op",
            "extra": "411116907 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "411116907 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "411116907 times\n4 procs"
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
          "id": "7ebd5603744b051ce669178e3843afbb09911e7c",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-02T06:45:21Z",
          "url": "https://github.com/volchanskyi/opengate/commit/7ebd5603744b051ce669178e3843afbb09911e7c"
        },
        "date": 1772433989831,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake",
            "value": 15034,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "76326 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - ns/op",
            "value": 15034,
            "unit": "ns/op",
            "extra": "76326 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "76326 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "76326 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent",
            "value": 155680,
            "unit": "ns/op\t   17539 B/op\t     309 allocs/op",
            "extra": "7695 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - ns/op",
            "value": 155680,
            "unit": "ns/op",
            "extra": "7695 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - B/op",
            "value": 17539,
            "unit": "B/op",
            "extra": "7695 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent - allocs/op",
            "value": 309,
            "unit": "allocs/op",
            "extra": "7695 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer",
            "value": 156051,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7406 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - ns/op",
            "value": 156051,
            "unit": "ns/op",
            "extra": "7406 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7406 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7406 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate",
            "value": 303422,
            "unit": "ns/op\t   28177 B/op\t     410 allocs/op",
            "extra": "4022 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - ns/op",
            "value": 303422,
            "unit": "ns/op",
            "extra": "4022 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - B/op",
            "value": 28177,
            "unit": "B/op",
            "extra": "4022 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4022 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load",
            "value": 52653,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "22410 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - ns/op",
            "value": 52653,
            "unit": "ns/op",
            "extra": "22410 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "22410 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22410 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice",
            "value": 423519,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "2697 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - ns/op",
            "value": 423519,
            "unit": "ns/op",
            "extra": "2697 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "2697 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "2697 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice",
            "value": 20735,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "58106 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - ns/op",
            "value": 20735,
            "unit": "ns/op",
            "extra": "58106 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "58106 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "58106 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices",
            "value": 152217,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7158 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - ns/op",
            "value": 152217,
            "unit": "ns/op",
            "extra": "7158 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7158 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7158 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus",
            "value": 457419,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "2461 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - ns/op",
            "value": 457419,
            "unit": "ns/op",
            "extra": "2461 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "2461 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "2461 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame",
            "value": 34.02,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34492714 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - ns/op",
            "value": 34.02,
            "unit": "ns/op",
            "extra": "34492714 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34492714 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34492714 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame",
            "value": 264.9,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4655852 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - ns/op",
            "value": 264.9,
            "unit": "ns/op",
            "extra": "4655852 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4655852 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4655852 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl",
            "value": 1212,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "922252 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - ns/op",
            "value": 1212,
            "unit": "ns/op",
            "extra": "922252 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "922252 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "922252 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl",
            "value": 1050,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl - ns/op",
            "value": 1050,
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
            "value": 37.09,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32402424 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - ns/op",
            "value": 37.09,
            "unit": "ns/op",
            "extra": "32402424 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32402424 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32402424 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello",
            "value": 3.281,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365483960 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - ns/op",
            "value": 3.281,
            "unit": "ns/op",
            "extra": "365483960 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365483960 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365483960 times\n4 procs"
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
          "id": "68c2f79d36d74b65495b08da8410f0e4e93e40cb",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-03T04:33:49Z",
          "url": "https://github.com/volchanskyi/opengate/commit/68c2f79d36d74b65495b08da8410f0e4e93e40cb"
        },
        "date": 1772512474758,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 14336,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "80979 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 14336,
            "unit": "ns/op",
            "extra": "80979 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "80979 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "80979 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 146891,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "8106 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 146891,
            "unit": "ns/op",
            "extra": "8106 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "8106 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "8106 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 147933,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7360 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 147933,
            "unit": "ns/op",
            "extra": "7360 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7360 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7360 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 285279,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "4202 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 285279,
            "unit": "ns/op",
            "extra": "4202 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "4202 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4202 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 49724,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "24213 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 49724,
            "unit": "ns/op",
            "extra": "24213 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "24213 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "24213 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 270938,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4362 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 270938,
            "unit": "ns/op",
            "extra": "4362 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4362 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4362 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 19624,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "60654 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 19624,
            "unit": "ns/op",
            "extra": "60654 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "60654 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "60654 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 143896,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7338 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 143896,
            "unit": "ns/op",
            "extra": "7338 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7338 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7338 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 238054,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4708 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 238054,
            "unit": "ns/op",
            "extra": "4708 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4708 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4708 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 32.28,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34813263 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 32.28,
            "unit": "ns/op",
            "extra": "34813263 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34813263 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34813263 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 251.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4831684 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 251.3,
            "unit": "ns/op",
            "extra": "4831684 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4831684 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4831684 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1132,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1132,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 977.5,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1229468 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 977.5,
            "unit": "ns/op",
            "extra": "1229468 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1229468 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1229468 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 36.38,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33899328 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 36.38,
            "unit": "ns/op",
            "extra": "33899328 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33899328 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33899328 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.155,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "386564814 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.155,
            "unit": "ns/op",
            "extra": "386564814 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "386564814 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "386564814 times\n4 procs"
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
          "id": "b27e73781882172894ac4974bc1ecc274936491f",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-03T04:49:02Z",
          "url": "https://github.com/volchanskyi/opengate/commit/b27e73781882172894ac4974bc1ecc274936491f"
        },
        "date": 1772513388274,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15002,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "78949 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15002,
            "unit": "ns/op",
            "extra": "78949 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "78949 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "78949 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 155379,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7257 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 155379,
            "unit": "ns/op",
            "extra": "7257 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7257 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7257 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 156364,
            "unit": "ns/op\t   17971 B/op\t     319 allocs/op",
            "extra": "7382 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 156364,
            "unit": "ns/op",
            "extra": "7382 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17971,
            "unit": "B/op",
            "extra": "7382 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7382 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 302497,
            "unit": "ns/op\t   28177 B/op\t     410 allocs/op",
            "extra": "4074 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 302497,
            "unit": "ns/op",
            "extra": "4074 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28177,
            "unit": "B/op",
            "extra": "4074 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4074 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 51774,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "23158 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 51774,
            "unit": "ns/op",
            "extra": "23158 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "23158 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "23158 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 373209,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3079 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 373209,
            "unit": "ns/op",
            "extra": "3079 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3079 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3079 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 20857,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56618 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 20857,
            "unit": "ns/op",
            "extra": "56618 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56618 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56618 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 150620,
            "unit": "ns/op\t   41697 B/op\t    1978 allocs/op",
            "extra": "7345 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 150620,
            "unit": "ns/op",
            "extra": "7345 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41697,
            "unit": "B/op",
            "extra": "7345 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7345 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 344948,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3207 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 344948,
            "unit": "ns/op",
            "extra": "3207 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3207 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3207 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 34.23,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35164143 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 34.23,
            "unit": "ns/op",
            "extra": "35164143 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35164143 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35164143 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 258.8,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4573290 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 258.8,
            "unit": "ns/op",
            "extra": "4573290 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4573290 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4573290 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1199,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "947726 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1199,
            "unit": "ns/op",
            "extra": "947726 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "947726 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "947726 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1047,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1047,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 36.57,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32035792 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 36.57,
            "unit": "ns/op",
            "extra": "32035792 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32035792 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32035792 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.282,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "366049687 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.282,
            "unit": "ns/op",
            "extra": "366049687 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "366049687 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "366049687 times\n4 procs"
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
          "id": "e28843d6315e934dc90807dec47e7c185ce1e75c",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T06:18:48Z",
          "url": "https://github.com/volchanskyi/opengate/commit/e28843d6315e934dc90807dec47e7c185ce1e75c"
        },
        "date": 1772605169203,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 14932,
            "unit": "ns/op\t    5417 B/op\t      62 allocs/op",
            "extra": "79987 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 14932,
            "unit": "ns/op",
            "extra": "79987 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5417,
            "unit": "B/op",
            "extra": "79987 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "79987 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 155962,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7495 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 155962,
            "unit": "ns/op",
            "extra": "7495 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7495 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7495 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 158643,
            "unit": "ns/op\t   17973 B/op\t     319 allocs/op",
            "extra": "7053 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 158643,
            "unit": "ns/op",
            "extra": "7053 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17973,
            "unit": "B/op",
            "extra": "7053 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7053 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 232769,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "5112 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 232769,
            "unit": "ns/op",
            "extra": "5112 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "5112 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "5112 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 36248,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "32848 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 36248,
            "unit": "ns/op",
            "extra": "32848 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "32848 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "32848 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 188729,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "6714 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 188729,
            "unit": "ns/op",
            "extra": "6714 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "6714 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "6714 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 17163,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "69784 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 17163,
            "unit": "ns/op",
            "extra": "69784 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "69784 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "69784 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 166727,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6594 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 166727,
            "unit": "ns/op",
            "extra": "6594 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6594 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6594 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 159455,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "6775 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 159455,
            "unit": "ns/op",
            "extra": "6775 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "6775 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "6775 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 32.14,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "36488390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 32.14,
            "unit": "ns/op",
            "extra": "36488390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "36488390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "36488390 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 282.5,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "3879966 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 282.5,
            "unit": "ns/op",
            "extra": "3879966 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "3879966 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "3879966 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1178,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "991672 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1178,
            "unit": "ns/op",
            "extra": "991672 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "991672 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "991672 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1009,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1009,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 35.53,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "34281315 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 35.53,
            "unit": "ns/op",
            "extra": "34281315 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "34281315 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34281315 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 2.887,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "415097914 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 2.887,
            "unit": "ns/op",
            "extra": "415097914 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "415097914 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "415097914 times\n4 procs"
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
          "id": "588c53aba44f60f66769d36a1a18e1e50a26ead9",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T07:25:27Z",
          "url": "https://github.com/volchanskyi/opengate/commit/588c53aba44f60f66769d36a1a18e1e50a26ead9"
        },
        "date": 1772609223155,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 16473,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "68804 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 16473,
            "unit": "ns/op",
            "extra": "68804 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "68804 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "68804 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 158142,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7492 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 158142,
            "unit": "ns/op",
            "extra": "7492 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7492 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7492 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 159083,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7382 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 159083,
            "unit": "ns/op",
            "extra": "7382 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7382 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7382 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 312854,
            "unit": "ns/op\t   28163 B/op\t     409 allocs/op",
            "extra": "3906 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 312854,
            "unit": "ns/op",
            "extra": "3906 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28163,
            "unit": "B/op",
            "extra": "3906 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3906 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 54115,
            "unit": "ns/op\t    7224 B/op\t      80 allocs/op",
            "extra": "22144 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 54115,
            "unit": "ns/op",
            "extra": "22144 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7224,
            "unit": "B/op",
            "extra": "22144 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22144 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 315332,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3490 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 315332,
            "unit": "ns/op",
            "extra": "3490 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3490 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3490 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 21035,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56085 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 21035,
            "unit": "ns/op",
            "extra": "56085 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56085 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56085 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 154283,
            "unit": "ns/op\t   41697 B/op\t    1978 allocs/op",
            "extra": "7203 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 154283,
            "unit": "ns/op",
            "extra": "7203 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41697,
            "unit": "B/op",
            "extra": "7203 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7203 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 324301,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3364 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 324301,
            "unit": "ns/op",
            "extra": "3364 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3364 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3364 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.63,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34146043 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.63,
            "unit": "ns/op",
            "extra": "34146043 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34146043 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34146043 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 276.6,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4455932 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 276.6,
            "unit": "ns/op",
            "extra": "4455932 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4455932 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4455932 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1229,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "980796 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1229,
            "unit": "ns/op",
            "extra": "980796 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "980796 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "980796 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1070,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "954903 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1070,
            "unit": "ns/op",
            "extra": "954903 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "954903 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "954903 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 37.53,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32342739 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 37.53,
            "unit": "ns/op",
            "extra": "32342739 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32342739 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32342739 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.28,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "364893652 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.28,
            "unit": "ns/op",
            "extra": "364893652 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "364893652 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "364893652 times\n4 procs"
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
          "id": "797a71afadc4ac11f29e0a8104a6b3ed73d6735d",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T17:55:41Z",
          "url": "https://github.com/volchanskyi/opengate/commit/797a71afadc4ac11f29e0a8104a6b3ed73d6735d"
        },
        "date": 1772647001745,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15515,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "77876 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15515,
            "unit": "ns/op",
            "extra": "77876 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "77876 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "77876 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 161641,
            "unit": "ns/op\t   17541 B/op\t     310 allocs/op",
            "extra": "7363 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 161641,
            "unit": "ns/op",
            "extra": "7363 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17541,
            "unit": "B/op",
            "extra": "7363 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7363 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 160301,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7363 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 160301,
            "unit": "ns/op",
            "extra": "7363 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7363 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7363 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 244337,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "4988 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 244337,
            "unit": "ns/op",
            "extra": "4988 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "4988 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4988 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 38889,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "30000 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 38889,
            "unit": "ns/op",
            "extra": "30000 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "30000 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "30000 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 671635,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "2658 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 671635,
            "unit": "ns/op",
            "extra": "2658 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "2658 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "2658 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 17552,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "68626 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 17552,
            "unit": "ns/op",
            "extra": "68626 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "68626 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "68626 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 173701,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6774 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 173701,
            "unit": "ns/op",
            "extra": "6774 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6774 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6774 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 233359,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4674 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 233359,
            "unit": "ns/op",
            "extra": "4674 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4674 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4674 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 32.85,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35638765 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 32.85,
            "unit": "ns/op",
            "extra": "35638765 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35638765 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35638765 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 286.8,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4186791 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 286.8,
            "unit": "ns/op",
            "extra": "4186791 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4186791 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4186791 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1264,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "902124 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1264,
            "unit": "ns/op",
            "extra": "902124 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "902124 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "902124 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1073,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1073,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 35.71,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32560501 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 35.71,
            "unit": "ns/op",
            "extra": "32560501 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32560501 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32560501 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 2.895,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "414709353 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 2.895,
            "unit": "ns/op",
            "extra": "414709353 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "414709353 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "414709353 times\n4 procs"
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
          "id": "0bce9dd41f777c12a183f6d4905c9177b4e7596b",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T18:04:32Z",
          "url": "https://github.com/volchanskyi/opengate/commit/0bce9dd41f777c12a183f6d4905c9177b4e7596b"
        },
        "date": 1772647528234,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 16084,
            "unit": "ns/op\t    5401 B/op\t      62 allocs/op",
            "extra": "74280 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 16084,
            "unit": "ns/op",
            "extra": "74280 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5401,
            "unit": "B/op",
            "extra": "74280 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "74280 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 157153,
            "unit": "ns/op\t   17541 B/op\t     310 allocs/op",
            "extra": "7123 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 157153,
            "unit": "ns/op",
            "extra": "7123 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17541,
            "unit": "B/op",
            "extra": "7123 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7123 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 157516,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7598 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 157516,
            "unit": "ns/op",
            "extra": "7598 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7598 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7598 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 310945,
            "unit": "ns/op\t   28162 B/op\t     410 allocs/op",
            "extra": "4000 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 310945,
            "unit": "ns/op",
            "extra": "4000 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28162,
            "unit": "B/op",
            "extra": "4000 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4000 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 52902,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22740 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 52902,
            "unit": "ns/op",
            "extra": "22740 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22740 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22740 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 279927,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "4011 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 279927,
            "unit": "ns/op",
            "extra": "4011 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "4011 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4011 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 20817,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57291 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 20817,
            "unit": "ns/op",
            "extra": "57291 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57291 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57291 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 150868,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6982 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 150868,
            "unit": "ns/op",
            "extra": "6982 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6982 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6982 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 302080,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3468 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 302080,
            "unit": "ns/op",
            "extra": "3468 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3468 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3468 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 34.39,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34394922 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 34.39,
            "unit": "ns/op",
            "extra": "34394922 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34394922 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34394922 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 262.2,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4551537 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 262.2,
            "unit": "ns/op",
            "extra": "4551537 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4551537 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4551537 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1221,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "980161 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1221,
            "unit": "ns/op",
            "extra": "980161 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "980161 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "980161 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1041,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1041,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 36.99,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32509744 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 36.99,
            "unit": "ns/op",
            "extra": "32509744 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32509744 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32509744 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.333,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365760843 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.333,
            "unit": "ns/op",
            "extra": "365760843 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365760843 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365760843 times\n4 procs"
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
          "id": "7235665c0873f9a3574103885bde75b421d0afde",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T19:04:59Z",
          "url": "https://github.com/volchanskyi/opengate/commit/7235665c0873f9a3574103885bde75b421d0afde"
        },
        "date": 1772651175988,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15122,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "77810 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15122,
            "unit": "ns/op",
            "extra": "77810 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "77810 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "77810 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 155648,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7261 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 155648,
            "unit": "ns/op",
            "extra": "7261 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7261 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7261 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 155787,
            "unit": "ns/op\t   17972 B/op\t     318 allocs/op",
            "extra": "7724 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 155787,
            "unit": "ns/op",
            "extra": "7724 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7724 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 318,
            "unit": "allocs/op",
            "extra": "7724 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 322581,
            "unit": "ns/op\t   28174 B/op\t     409 allocs/op",
            "extra": "4005 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 322581,
            "unit": "ns/op",
            "extra": "4005 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28174,
            "unit": "B/op",
            "extra": "4005 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "4005 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 53407,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22406 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 53407,
            "unit": "ns/op",
            "extra": "22406 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22406 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22406 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 285191,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "4107 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 285191,
            "unit": "ns/op",
            "extra": "4107 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "4107 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4107 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 20992,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57015 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 20992,
            "unit": "ns/op",
            "extra": "57015 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57015 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57015 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 151220,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6792 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 151220,
            "unit": "ns/op",
            "extra": "6792 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6792 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6792 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 283142,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3908 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 283142,
            "unit": "ns/op",
            "extra": "3908 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3908 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3908 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.58,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34541815 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.58,
            "unit": "ns/op",
            "extra": "34541815 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34541815 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34541815 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 260.1,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4593382 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 260.1,
            "unit": "ns/op",
            "extra": "4593382 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4593382 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4593382 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1215,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "907927 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1215,
            "unit": "ns/op",
            "extra": "907927 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "907927 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "907927 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1072,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1072,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 36.99,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31162046 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 36.99,
            "unit": "ns/op",
            "extra": "31162046 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31162046 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31162046 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.292,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "360254479 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.292,
            "unit": "ns/op",
            "extra": "360254479 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "360254479 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "360254479 times\n4 procs"
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
          "id": "ed48f9f86ab855cf25eace4d31f38c94ab6299a4",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T20:05:34Z",
          "url": "https://github.com/volchanskyi/opengate/commit/ed48f9f86ab855cf25eace4d31f38c94ab6299a4"
        },
        "date": 1772654798600,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15580,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "75986 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15580,
            "unit": "ns/op",
            "extra": "75986 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "75986 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "75986 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 158409,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7538 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 158409,
            "unit": "ns/op",
            "extra": "7538 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7538 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7538 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 157037,
            "unit": "ns/op\t   17973 B/op\t     319 allocs/op",
            "extra": "7483 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 157037,
            "unit": "ns/op",
            "extra": "7483 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17973,
            "unit": "B/op",
            "extra": "7483 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7483 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 305114,
            "unit": "ns/op\t   28175 B/op\t     409 allocs/op",
            "extra": "3973 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 305114,
            "unit": "ns/op",
            "extra": "3973 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "3973 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3973 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 53851,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22521 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 53851,
            "unit": "ns/op",
            "extra": "22521 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22521 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22521 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 280730,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3814 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 280730,
            "unit": "ns/op",
            "extra": "3814 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3814 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3814 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 20763,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57687 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 20763,
            "unit": "ns/op",
            "extra": "57687 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57687 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57687 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 151140,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "7006 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 151140,
            "unit": "ns/op",
            "extra": "7006 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "7006 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "7006 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 269152,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4122 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 269152,
            "unit": "ns/op",
            "extra": "4122 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4122 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4122 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.64,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35336173 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.64,
            "unit": "ns/op",
            "extra": "35336173 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35336173 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35336173 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 268.1,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4567014 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 268.1,
            "unit": "ns/op",
            "extra": "4567014 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4567014 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4567014 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1226,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "934834 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1226,
            "unit": "ns/op",
            "extra": "934834 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "934834 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "934834 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1066,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "943346 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1066,
            "unit": "ns/op",
            "extra": "943346 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "943346 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "943346 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 37.92,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31642087 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 37.92,
            "unit": "ns/op",
            "extra": "31642087 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31642087 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31642087 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.283,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365158712 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.283,
            "unit": "ns/op",
            "extra": "365158712 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365158712 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365158712 times\n4 procs"
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
          "id": "0fc7357412bc9dad0c3e79faafc4bc10e7f2505e",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T21:33:23Z",
          "url": "https://github.com/volchanskyi/opengate/commit/0fc7357412bc9dad0c3e79faafc4bc10e7f2505e"
        },
        "date": 1772660065778,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15424,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "76600 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15424,
            "unit": "ns/op",
            "extra": "76600 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "76600 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "76600 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 159060,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7446 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 159060,
            "unit": "ns/op",
            "extra": "7446 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7446 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7446 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 159141,
            "unit": "ns/op\t   17973 B/op\t     319 allocs/op",
            "extra": "7399 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 159141,
            "unit": "ns/op",
            "extra": "7399 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17973,
            "unit": "B/op",
            "extra": "7399 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7399 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 308778,
            "unit": "ns/op\t   28162 B/op\t     409 allocs/op",
            "extra": "3894 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 308778,
            "unit": "ns/op",
            "extra": "3894 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28162,
            "unit": "B/op",
            "extra": "3894 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "3894 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 53863,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "21852 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 53863,
            "unit": "ns/op",
            "extra": "21852 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "21852 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "21852 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 305830,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3883 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 305830,
            "unit": "ns/op",
            "extra": "3883 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3883 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3883 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 21128,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "56632 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 21128,
            "unit": "ns/op",
            "extra": "56632 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "56632 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "56632 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 160632,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6396 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 160632,
            "unit": "ns/op",
            "extra": "6396 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6396 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6396 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 310051,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3459 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 310051,
            "unit": "ns/op",
            "extra": "3459 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3459 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3459 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.72,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34586266 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.72,
            "unit": "ns/op",
            "extra": "34586266 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34586266 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34586266 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 262.2,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4592712 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 262.2,
            "unit": "ns/op",
            "extra": "4592712 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4592712 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4592712 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1220,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "958279 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1220,
            "unit": "ns/op",
            "extra": "958279 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "958279 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "958279 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1052,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1052,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 37,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31927682 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 37,
            "unit": "ns/op",
            "extra": "31927682 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31927682 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31927682 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.276,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365793416 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.276,
            "unit": "ns/op",
            "extra": "365793416 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365793416 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365793416 times\n4 procs"
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
          "id": "d1aeb738d6d73e84b73754e2704980326309abf5",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T22:00:06Z",
          "url": "https://github.com/volchanskyi/opengate/commit/d1aeb738d6d73e84b73754e2704980326309abf5"
        },
        "date": 1772661670013,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15859,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "75217 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15859,
            "unit": "ns/op",
            "extra": "75217 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "75217 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "75217 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 157426,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7268 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 157426,
            "unit": "ns/op",
            "extra": "7268 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7268 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7268 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 158688,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "6794 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 158688,
            "unit": "ns/op",
            "extra": "6794 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "6794 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "6794 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 238980,
            "unit": "ns/op\t   28175 B/op\t     409 allocs/op",
            "extra": "4998 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 238980,
            "unit": "ns/op",
            "extra": "4998 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "4998 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 409,
            "unit": "allocs/op",
            "extra": "4998 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 36982,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "32194 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 36982,
            "unit": "ns/op",
            "extra": "32194 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "32194 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "32194 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 290293,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4435 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 290293,
            "unit": "ns/op",
            "extra": "4435 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4435 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4435 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 17411,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "68130 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 17411,
            "unit": "ns/op",
            "extra": "68130 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "68130 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "68130 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 170678,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6609 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 170678,
            "unit": "ns/op",
            "extra": "6609 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6609 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6609 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 284548,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3622 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 284548,
            "unit": "ns/op",
            "extra": "3622 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3622 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3622 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 32.05,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "36857349 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 32.05,
            "unit": "ns/op",
            "extra": "36857349 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "36857349 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "36857349 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 274.9,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4331014 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 274.9,
            "unit": "ns/op",
            "extra": "4331014 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4331014 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4331014 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1274,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "982699 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1274,
            "unit": "ns/op",
            "extra": "982699 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "982699 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "982699 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1063,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "995698 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1063,
            "unit": "ns/op",
            "extra": "995698 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "995698 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "995698 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 35.96,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33394104 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 35.96,
            "unit": "ns/op",
            "extra": "33394104 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33394104 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33394104 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 2.937,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "415170603 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 2.937,
            "unit": "ns/op",
            "extra": "415170603 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "415170603 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "415170603 times\n4 procs"
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
          "id": "a59a1652d6b2d57bd05d67b155d08b7ac4b49ee3",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-04T22:13:50Z",
          "url": "https://github.com/volchanskyi/opengate/commit/a59a1652d6b2d57bd05d67b155d08b7ac4b49ee3"
        },
        "date": 1772662496324,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 16593,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "72940 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 16593,
            "unit": "ns/op",
            "extra": "72940 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "72940 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "72940 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 157454,
            "unit": "ns/op\t   17539 B/op\t     309 allocs/op",
            "extra": "7201 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 157454,
            "unit": "ns/op",
            "extra": "7201 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17539,
            "unit": "B/op",
            "extra": "7201 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 309,
            "unit": "allocs/op",
            "extra": "7201 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 158152,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7659 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 158152,
            "unit": "ns/op",
            "extra": "7659 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7659 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7659 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 306036,
            "unit": "ns/op\t   28174 B/op\t     410 allocs/op",
            "extra": "3994 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 306036,
            "unit": "ns/op",
            "extra": "3994 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28174,
            "unit": "B/op",
            "extra": "3994 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "3994 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 54700,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22500 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 54700,
            "unit": "ns/op",
            "extra": "22500 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22500 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22500 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 304418,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "4186 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 304418,
            "unit": "ns/op",
            "extra": "4186 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "4186 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4186 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 21209,
            "unit": "ns/op\t    1560 B/op\t      60 allocs/op",
            "extra": "57872 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 21209,
            "unit": "ns/op",
            "extra": "57872 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1560,
            "unit": "B/op",
            "extra": "57872 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "57872 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 157259,
            "unit": "ns/op\t   41696 B/op\t    1978 allocs/op",
            "extra": "6806 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 157259,
            "unit": "ns/op",
            "extra": "6806 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 41696,
            "unit": "B/op",
            "extra": "6806 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1978,
            "unit": "allocs/op",
            "extra": "6806 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 931660,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "2348 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 931660,
            "unit": "ns/op",
            "extra": "2348 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "2348 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "2348 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.78,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34692159 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.78,
            "unit": "ns/op",
            "extra": "34692159 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34692159 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34692159 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 263.1,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4490498 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 263.1,
            "unit": "ns/op",
            "extra": "4490498 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4490498 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4490498 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1222,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "993259 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1222,
            "unit": "ns/op",
            "extra": "993259 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "993259 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "993259 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1054,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1054,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 36.98,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "31943797 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 36.98,
            "unit": "ns/op",
            "extra": "31943797 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "31943797 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31943797 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.279,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365243631 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.279,
            "unit": "ns/op",
            "extra": "365243631 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365243631 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365243631 times\n4 procs"
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
          "id": "abce967b4861ec8e17671a0ec9f1948d60d364c4",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-05T02:58:17Z",
          "url": "https://github.com/volchanskyi/opengate/commit/abce967b4861ec8e17671a0ec9f1948d60d364c4"
        },
        "date": 1772679562721,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15131,
            "unit": "ns/op\t    5400 B/op\t      62 allocs/op",
            "extra": "77605 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15131,
            "unit": "ns/op",
            "extra": "77605 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5400,
            "unit": "B/op",
            "extra": "77605 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "77605 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 154973,
            "unit": "ns/op\t   17541 B/op\t     310 allocs/op",
            "extra": "7302 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 154973,
            "unit": "ns/op",
            "extra": "7302 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17541,
            "unit": "B/op",
            "extra": "7302 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7302 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 154987,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7747 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 154987,
            "unit": "ns/op",
            "extra": "7747 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7747 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7747 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 305909,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "4058 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 305909,
            "unit": "ns/op",
            "extra": "4058 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "4058 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4058 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 51919,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "23224 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 51919,
            "unit": "ns/op",
            "extra": "23224 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "23224 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "23224 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 283999,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4248 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 283999,
            "unit": "ns/op",
            "extra": "4248 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4248 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4248 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 20492,
            "unit": "ns/op\t    1712 B/op\t      62 allocs/op",
            "extra": "58321 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 20492,
            "unit": "ns/op",
            "extra": "58321 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1712,
            "unit": "B/op",
            "extra": "58321 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "58321 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 154649,
            "unit": "ns/op\t   48097 B/op\t    2028 allocs/op",
            "extra": "7851 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 154649,
            "unit": "ns/op",
            "extra": "7851 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 48097,
            "unit": "B/op",
            "extra": "7851 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 2028,
            "unit": "allocs/op",
            "extra": "7851 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 277922,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3928 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 277922,
            "unit": "ns/op",
            "extra": "3928 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3928 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3928 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.62,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35217655 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.62,
            "unit": "ns/op",
            "extra": "35217655 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35217655 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35217655 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 259.9,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4642090 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 259.9,
            "unit": "ns/op",
            "extra": "4642090 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4642090 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4642090 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1208,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "942066 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1208,
            "unit": "ns/op",
            "extra": "942066 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "942066 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "942066 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1034,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1034,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 38.05,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32279683 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 38.05,
            "unit": "ns/op",
            "extra": "32279683 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32279683 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32279683 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.275,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "366010670 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.275,
            "unit": "ns/op",
            "extra": "366010670 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "366010670 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "366010670 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "volchanskyi",
            "username": "volchanskyi",
            "email": "ivan.volchanskyi@gmail.com"
          },
          "committer": {
            "name": "GitHub",
            "username": "web-flow",
            "email": "noreply@github.com"
          },
          "id": "fa44822a9c955b4a8ee54fa2f91e2647ec32dcac",
          "message": "Merge pull request #28 from volchanskyi/add-claude-github-actions-1772766704454\n\nAdd Claude Code GitHub Workflow",
          "timestamp": "2026-03-06T03:16:56Z",
          "url": "https://github.com/volchanskyi/opengate/commit/fa44822a9c955b4a8ee54fa2f91e2647ec32dcac"
        },
        "date": 1772770155336,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15893,
            "unit": "ns/op\t    5417 B/op\t      62 allocs/op",
            "extra": "73501 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15893,
            "unit": "ns/op",
            "extra": "73501 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5417,
            "unit": "B/op",
            "extra": "73501 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "73501 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 156916,
            "unit": "ns/op\t   17541 B/op\t     310 allocs/op",
            "extra": "7510 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 156916,
            "unit": "ns/op",
            "extra": "7510 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17541,
            "unit": "B/op",
            "extra": "7510 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7510 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 157398,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7240 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 157398,
            "unit": "ns/op",
            "extra": "7240 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7240 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7240 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 316185,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "4070 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 316185,
            "unit": "ns/op",
            "extra": "4070 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "4070 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4070 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 53485,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22318 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 53485,
            "unit": "ns/op",
            "extra": "22318 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22318 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22318 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 288263,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4294 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 288263,
            "unit": "ns/op",
            "extra": "4294 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4294 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4294 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 21228,
            "unit": "ns/op\t    1712 B/op\t      62 allocs/op",
            "extra": "56014 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 21228,
            "unit": "ns/op",
            "extra": "56014 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1712,
            "unit": "B/op",
            "extra": "56014 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "56014 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 151555,
            "unit": "ns/op\t   48097 B/op\t    2028 allocs/op",
            "extra": "7731 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 151555,
            "unit": "ns/op",
            "extra": "7731 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 48097,
            "unit": "B/op",
            "extra": "7731 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 2028,
            "unit": "allocs/op",
            "extra": "7731 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 285976,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3686 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 285976,
            "unit": "ns/op",
            "extra": "3686 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3686 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3686 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.78,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "31419748 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.78,
            "unit": "ns/op",
            "extra": "31419748 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "31419748 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "31419748 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 261.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4573838 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 261.3,
            "unit": "ns/op",
            "extra": "4573838 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4573838 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4573838 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1201,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "950486 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1201,
            "unit": "ns/op",
            "extra": "950486 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "950486 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "950486 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1054,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1054,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 36.92,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32439270 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 36.92,
            "unit": "ns/op",
            "extra": "32439270 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32439270 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32439270 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.277,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365774928 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.277,
            "unit": "ns/op",
            "extra": "365774928 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365774928 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365774928 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "name": "volchanskyi",
            "username": "volchanskyi",
            "email": "ivan.volchanskyi@gmail.com"
          },
          "committer": {
            "name": "GitHub",
            "username": "web-flow",
            "email": "noreply@github.com"
          },
          "id": "fa44822a9c955b4a8ee54fa2f91e2647ec32dcac",
          "message": "Merge pull request #28 from volchanskyi/add-claude-github-actions-1772766704454\n\nAdd Claude Code GitHub Workflow",
          "timestamp": "2026-03-06T03:16:56Z",
          "url": "https://github.com/volchanskyi/opengate/commit/fa44822a9c955b4a8ee54fa2f91e2647ec32dcac"
        },
        "date": 1772770237785,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15591,
            "unit": "ns/op\t    5417 B/op\t      62 allocs/op",
            "extra": "74359 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15591,
            "unit": "ns/op",
            "extra": "74359 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5417,
            "unit": "B/op",
            "extra": "74359 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "74359 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 156130,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7650 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 156130,
            "unit": "ns/op",
            "extra": "7650 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7650 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7650 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 155842,
            "unit": "ns/op\t   17971 B/op\t     318 allocs/op",
            "extra": "7334 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 155842,
            "unit": "ns/op",
            "extra": "7334 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17971,
            "unit": "B/op",
            "extra": "7334 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 318,
            "unit": "allocs/op",
            "extra": "7334 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 308141,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "4042 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 308141,
            "unit": "ns/op",
            "extra": "4042 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "4042 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4042 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 52494,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "23012 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 52494,
            "unit": "ns/op",
            "extra": "23012 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "23012 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "23012 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 289061,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "4029 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 289061,
            "unit": "ns/op",
            "extra": "4029 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "4029 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4029 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 21023,
            "unit": "ns/op\t    1712 B/op\t      62 allocs/op",
            "extra": "56694 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 21023,
            "unit": "ns/op",
            "extra": "56694 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1712,
            "unit": "B/op",
            "extra": "56694 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "56694 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 155269,
            "unit": "ns/op\t   48096 B/op\t    2028 allocs/op",
            "extra": "7650 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 155269,
            "unit": "ns/op",
            "extra": "7650 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 48096,
            "unit": "B/op",
            "extra": "7650 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 2028,
            "unit": "allocs/op",
            "extra": "7650 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 288151,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "3754 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 288151,
            "unit": "ns/op",
            "extra": "3754 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "3754 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "3754 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.49,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34655744 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.49,
            "unit": "ns/op",
            "extra": "34655744 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34655744 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34655744 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 263.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4546545 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 263.3,
            "unit": "ns/op",
            "extra": "4546545 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4546545 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4546545 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1211,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "998394 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1211,
            "unit": "ns/op",
            "extra": "998394 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "998394 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "998394 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1043,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1043,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 36.72,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "30702590 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 36.72,
            "unit": "ns/op",
            "extra": "30702590 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "30702590 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "30702590 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.272,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "366133848 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.272,
            "unit": "ns/op",
            "extra": "366133848 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "366133848 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "366133848 times\n4 procs"
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
          "id": "0e4ffc626de606995b0b3bb8de80eb8fceb15303",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-06T07:00:02Z",
          "url": "https://github.com/volchanskyi/opengate/commit/0e4ffc626de606995b0b3bb8de80eb8fceb15303"
        },
        "date": 1772780458623,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15443,
            "unit": "ns/op\t    5416 B/op\t      62 allocs/op",
            "extra": "75247 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15443,
            "unit": "ns/op",
            "extra": "75247 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5416,
            "unit": "B/op",
            "extra": "75247 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "75247 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 154346,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7298 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 154346,
            "unit": "ns/op",
            "extra": "7298 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7298 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7298 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 154920,
            "unit": "ns/op\t   17972 B/op\t     319 allocs/op",
            "extra": "7381 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 154920,
            "unit": "ns/op",
            "extra": "7381 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17972,
            "unit": "B/op",
            "extra": "7381 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7381 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 300104,
            "unit": "ns/op\t   28176 B/op\t     410 allocs/op",
            "extra": "3919 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 300104,
            "unit": "ns/op",
            "extra": "3919 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28176,
            "unit": "B/op",
            "extra": "3919 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "3919 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 51678,
            "unit": "ns/op\t    7304 B/op\t      80 allocs/op",
            "extra": "23104 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 51678,
            "unit": "ns/op",
            "extra": "23104 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7304,
            "unit": "B/op",
            "extra": "23104 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "23104 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 280941,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "4479 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 280941,
            "unit": "ns/op",
            "extra": "4479 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "4479 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "4479 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 20479,
            "unit": "ns/op\t    1712 B/op\t      62 allocs/op",
            "extra": "57602 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 20479,
            "unit": "ns/op",
            "extra": "57602 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1712,
            "unit": "B/op",
            "extra": "57602 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "57602 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 149801,
            "unit": "ns/op\t   48096 B/op\t    2028 allocs/op",
            "extra": "7474 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 149801,
            "unit": "ns/op",
            "extra": "7474 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 48096,
            "unit": "B/op",
            "extra": "7474 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 2028,
            "unit": "allocs/op",
            "extra": "7474 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 259077,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "4304 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 259077,
            "unit": "ns/op",
            "extra": "4304 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "4304 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "4304 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.62,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "35247676 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.62,
            "unit": "ns/op",
            "extra": "35247676 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "35247676 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "35247676 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 259.8,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4611138 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 259.8,
            "unit": "ns/op",
            "extra": "4611138 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4611138 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4611138 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1202,
            "unit": "ns/op\t     576 B/op\t      17 allocs/op",
            "extra": "949471 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1202,
            "unit": "ns/op",
            "extra": "949471 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 576,
            "unit": "B/op",
            "extra": "949471 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "949471 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1037,
            "unit": "ns/op\t     536 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1037,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 536,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 36.64,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "33142806 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 36.64,
            "unit": "ns/op",
            "extra": "33142806 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "33142806 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "33142806 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.275,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "366556480 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.275,
            "unit": "ns/op",
            "extra": "366556480 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "366556480 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "366556480 times\n4 procs"
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
          "id": "d9a97e0fee9d2923b11422c0443e9f9c0968e2f0",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-06T15:27:57Z",
          "url": "https://github.com/volchanskyi/opengate/commit/d9a97e0fee9d2923b11422c0443e9f9c0968e2f0"
        },
        "date": 1772811030300,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15613,
            "unit": "ns/op\t    5401 B/op\t      62 allocs/op",
            "extra": "73009 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15613,
            "unit": "ns/op",
            "extra": "73009 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5401,
            "unit": "B/op",
            "extra": "73009 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "73009 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 156392,
            "unit": "ns/op\t   17540 B/op\t     310 allocs/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 156392,
            "unit": "ns/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17540,
            "unit": "B/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 310,
            "unit": "allocs/op",
            "extra": "7486 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 156373,
            "unit": "ns/op\t   17973 B/op\t     319 allocs/op",
            "extra": "7539 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 156373,
            "unit": "ns/op",
            "extra": "7539 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17973,
            "unit": "B/op",
            "extra": "7539 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 319,
            "unit": "allocs/op",
            "extra": "7539 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 307978,
            "unit": "ns/op\t   28175 B/op\t     410 allocs/op",
            "extra": "4118 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 307978,
            "unit": "ns/op",
            "extra": "4118 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 28175,
            "unit": "B/op",
            "extra": "4118 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 410,
            "unit": "allocs/op",
            "extra": "4118 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 52306,
            "unit": "ns/op\t    7320 B/op\t      80 allocs/op",
            "extra": "22812 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 52306,
            "unit": "ns/op",
            "extra": "22812 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7320,
            "unit": "B/op",
            "extra": "22812 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "22812 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 492048,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "2556 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 492048,
            "unit": "ns/op",
            "extra": "2556 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "2556 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "2556 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 20851,
            "unit": "ns/op\t    1712 B/op\t      62 allocs/op",
            "extra": "56678 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 20851,
            "unit": "ns/op",
            "extra": "56678 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1712,
            "unit": "B/op",
            "extra": "56678 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 62,
            "unit": "allocs/op",
            "extra": "56678 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 152729,
            "unit": "ns/op\t   48096 B/op\t    2028 allocs/op",
            "extra": "6902 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 152729,
            "unit": "ns/op",
            "extra": "6902 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 48096,
            "unit": "B/op",
            "extra": "6902 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 2028,
            "unit": "allocs/op",
            "extra": "6902 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 629296,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "1687 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 629296,
            "unit": "ns/op",
            "extra": "1687 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "1687 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "1687 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 34.06,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "34639876 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 34.06,
            "unit": "ns/op",
            "extra": "34639876 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "34639876 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34639876 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 261.3,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4621461 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 261.3,
            "unit": "ns/op",
            "extra": "4621461 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4621461 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4621461 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1724,
            "unit": "ns/op\t     792 B/op\t      28 allocs/op",
            "extra": "647569 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1724,
            "unit": "ns/op",
            "extra": "647569 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 792,
            "unit": "B/op",
            "extra": "647569 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 28,
            "unit": "allocs/op",
            "extra": "647569 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1119,
            "unit": "ns/op\t     680 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1119,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 680,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 37.59,
            "unit": "ns/op\t      96 B/op\t       1 allocs/op",
            "extra": "32002281 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 37.59,
            "unit": "ns/op",
            "extra": "32002281 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "32002281 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "32002281 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.275,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "365515230 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.275,
            "unit": "ns/op",
            "extra": "365515230 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "365515230 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "365515230 times\n4 procs"
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
          "id": "6df549e1796c8e0f5444b29ec9e0133844a1bc2b",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-07T09:31:34Z",
          "url": "https://github.com/volchanskyi/opengate/commit/6df549e1796c8e0f5444b29ec9e0133844a1bc2b"
        },
        "date": 1772876065194,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 17326,
            "unit": "ns/op\t    5464 B/op\t      63 allocs/op",
            "extra": "67812 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 17326,
            "unit": "ns/op",
            "extra": "67812 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5464,
            "unit": "B/op",
            "extra": "67812 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 63,
            "unit": "allocs/op",
            "extra": "67812 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 156738,
            "unit": "ns/op\t   16836 B/op\t     292 allocs/op",
            "extra": "7497 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 156738,
            "unit": "ns/op",
            "extra": "7497 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 16836,
            "unit": "B/op",
            "extra": "7497 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 292,
            "unit": "allocs/op",
            "extra": "7497 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 157075,
            "unit": "ns/op\t   17189 B/op\t     300 allocs/op",
            "extra": "7494 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 157075,
            "unit": "ns/op",
            "extra": "7494 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17189,
            "unit": "B/op",
            "extra": "7494 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 300,
            "unit": "allocs/op",
            "extra": "7494 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 241537,
            "unit": "ns/op\t   27874 B/op\t     402 allocs/op",
            "extra": "5026 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 241537,
            "unit": "ns/op",
            "extra": "5026 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 27874,
            "unit": "B/op",
            "extra": "5026 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 402,
            "unit": "allocs/op",
            "extra": "5026 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 36401,
            "unit": "ns/op\t    7640 B/op\t      86 allocs/op",
            "extra": "32191 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 36401,
            "unit": "ns/op",
            "extra": "32191 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7640,
            "unit": "B/op",
            "extra": "32191 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 86,
            "unit": "allocs/op",
            "extra": "32191 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 201932,
            "unit": "ns/op\t     800 B/op\t      22 allocs/op",
            "extra": "5887 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 201932,
            "unit": "ns/op",
            "extra": "5887 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 800,
            "unit": "B/op",
            "extra": "5887 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "5887 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 17432,
            "unit": "ns/op\t    1624 B/op\t      56 allocs/op",
            "extra": "68407 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 17432,
            "unit": "ns/op",
            "extra": "68407 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1624,
            "unit": "B/op",
            "extra": "68407 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 56,
            "unit": "allocs/op",
            "extra": "68407 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 169114,
            "unit": "ns/op\t   43736 B/op\t    1725 allocs/op",
            "extra": "7035 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 169114,
            "unit": "ns/op",
            "extra": "7035 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 43736,
            "unit": "B/op",
            "extra": "7035 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1725,
            "unit": "allocs/op",
            "extra": "7035 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 184405,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "5610 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 184405,
            "unit": "ns/op",
            "extra": "5610 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "5610 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "5610 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 32.03,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "36518816 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 32.03,
            "unit": "ns/op",
            "extra": "36518816 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "36518816 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "36518816 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 289.7,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4109181 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 289.7,
            "unit": "ns/op",
            "extra": "4109181 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4109181 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4109181 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1696,
            "unit": "ns/op\t     792 B/op\t      28 allocs/op",
            "extra": "693475 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1696,
            "unit": "ns/op",
            "extra": "693475 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 792,
            "unit": "B/op",
            "extra": "693475 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 28,
            "unit": "allocs/op",
            "extra": "693475 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1153,
            "unit": "ns/op\t     680 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1153,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 680,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 2.003,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "605799093 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 2.003,
            "unit": "ns/op",
            "extra": "605799093 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "605799093 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "605799093 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.18,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "376372683 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.18,
            "unit": "ns/op",
            "extra": "376372683 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "376372683 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "376372683 times\n4 procs"
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
          "id": "a99dfe94eb314948b49431f6922882a44372d921",
          "message": "ci: auto-merge dev → main",
          "timestamp": "2026-03-07T09:46:38Z",
          "url": "https://github.com/volchanskyi/opengate/commit/a99dfe94eb314948b49431f6922882a44372d921"
        },
        "date": 1772876855624,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi)",
            "value": 15615,
            "unit": "ns/op\t    5449 B/op\t      63 allocs/op",
            "extra": "77146 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - ns/op",
            "value": 15615,
            "unit": "ns/op",
            "extra": "77146 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - B/op",
            "value": 5449,
            "unit": "B/op",
            "extra": "77146 times\n4 procs"
          },
          {
            "name": "BenchmarkHandshaker_PerformHandshake (github.com/volchanskyi/opengate/server/internal/agentapi) - allocs/op",
            "value": 63,
            "unit": "allocs/op",
            "extra": "77146 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 153914,
            "unit": "ns/op\t   16836 B/op\t     291 allocs/op",
            "extra": "7608 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 153914,
            "unit": "ns/op",
            "extra": "7608 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 16836,
            "unit": "B/op",
            "extra": "7608 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignAgent (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 291,
            "unit": "allocs/op",
            "extra": "7608 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 155069,
            "unit": "ns/op\t   17188 B/op\t     300 allocs/op",
            "extra": "7327 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 155069,
            "unit": "ns/op",
            "extra": "7327 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 17188,
            "unit": "B/op",
            "extra": "7327 times\n4 procs"
          },
          {
            "name": "BenchmarkManager_SignServer (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 300,
            "unit": "allocs/op",
            "extra": "7327 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 308733,
            "unit": "ns/op\t   27871 B/op\t     402 allocs/op",
            "extra": "4078 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 308733,
            "unit": "ns/op",
            "extra": "4078 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 27871,
            "unit": "B/op",
            "extra": "4078 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Generate (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 402,
            "unit": "allocs/op",
            "extra": "4078 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert)",
            "value": 51947,
            "unit": "ns/op\t    7624 B/op\t      86 allocs/op",
            "extra": "22784 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - ns/op",
            "value": 51947,
            "unit": "ns/op",
            "extra": "22784 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - B/op",
            "value": 7624,
            "unit": "B/op",
            "extra": "22784 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager_Load (github.com/volchanskyi/opengate/server/internal/cert) - allocs/op",
            "value": 86,
            "unit": "allocs/op",
            "extra": "22784 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 410518,
            "unit": "ns/op\t     799 B/op\t      22 allocs/op",
            "extra": "3289 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 410518,
            "unit": "ns/op",
            "extra": "3289 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 799,
            "unit": "B/op",
            "extra": "3289 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_UpsertDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 22,
            "unit": "allocs/op",
            "extra": "3289 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 20382,
            "unit": "ns/op\t    1624 B/op\t      56 allocs/op",
            "extra": "58984 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 20382,
            "unit": "ns/op",
            "extra": "58984 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 1624,
            "unit": "B/op",
            "extra": "58984 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_GetDevice (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 56,
            "unit": "allocs/op",
            "extra": "58984 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 149655,
            "unit": "ns/op\t   43736 B/op\t    1725 allocs/op",
            "extra": "7588 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 149655,
            "unit": "ns/op",
            "extra": "7588 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 43736,
            "unit": "B/op",
            "extra": "7588 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_ListDevices (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 1725,
            "unit": "allocs/op",
            "extra": "7588 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db)",
            "value": 424177,
            "unit": "ns/op\t     432 B/op\t      14 allocs/op",
            "extra": "2607 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - ns/op",
            "value": 424177,
            "unit": "ns/op",
            "extra": "2607 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - B/op",
            "value": 432,
            "unit": "B/op",
            "extra": "2607 times\n4 procs"
          },
          {
            "name": "BenchmarkStore_SetDeviceStatus (github.com/volchanskyi/opengate/server/internal/db) - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "2607 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 33.17,
            "unit": "ns/op\t       5 B/op\t       1 allocs/op",
            "extra": "36180654 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 33.17,
            "unit": "ns/op",
            "extra": "36180654 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 5,
            "unit": "B/op",
            "extra": "36180654 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_WriteFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "36180654 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 264,
            "unit": "ns/op\t    1080 B/op\t       4 allocs/op",
            "extra": "4484246 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 264,
            "unit": "ns/op",
            "extra": "4484246 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "4484246 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_ReadFrame (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "4484246 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1744,
            "unit": "ns/op\t     792 B/op\t      28 allocs/op",
            "extra": "664990 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1744,
            "unit": "ns/op",
            "extra": "664990 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 792,
            "unit": "B/op",
            "extra": "664990 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_EncodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 28,
            "unit": "allocs/op",
            "extra": "664990 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 1122,
            "unit": "ns/op\t     680 B/op\t      13 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 1122,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 680,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkCodec_DecodeControl (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 0.6259,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "1000000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 0.6259,
            "unit": "ns/op",
            "extra": "1000000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "1000000000 times\n4 procs"
          },
          {
            "name": "BenchmarkEncodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "1000000000 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol)",
            "value": 3.12,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "383597114 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - ns/op",
            "value": 3.12,
            "unit": "ns/op",
            "extra": "383597114 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "383597114 times\n4 procs"
          },
          {
            "name": "BenchmarkDecodeServerHello (github.com/volchanskyi/opengate/server/internal/protocol) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "383597114 times\n4 procs"
          }
        ]
      }
    ]
  }
}