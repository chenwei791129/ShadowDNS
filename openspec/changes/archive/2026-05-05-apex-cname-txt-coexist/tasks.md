## 1. Write failing integration test (TDD red phase)

- [x] 1.1 Add `TestQuery_Apex_CNAME_TXT_Coexist` to `test/integration/query_test.go` covering three sub-assertions against `example.com.` apex (loopback queries are matched by view-other per the package header comment): (a) `dns.TypeTXT` query returns the existing SPF TXT `"v=spf1 ip4:198.51.100.0/24 -all"` and answer section contains no CNAME RR, (b) `dns.TypeCNAME` query returns a single CNAME RR with target `www.example.com.`, (c) `dns.TypeA` query returns the existing apex A `198.51.100.10` and answer section contains no CNAME RR
- [x] 1.2 Run `go test -race -count=1 ./test/integration/... -run TestQuery_Apex_CNAME_TXT_Coexist` and confirm the CNAME sub-assertion fails (apex CNAME does not yet exist in the fixture)

## 2. Update zone fixture (TDD green phase)

- [x] 2.1 Edit BOTH `testdata/integration/master/example.com_view-other.fwd` (queried by loopback in integration tests) AND `testdata/integration/master/example.com_view-th.fwd` (kept symmetric with view-other) to add `@         IN CNAME www.example.com.` alongside the existing apex SOA / NS / A / AAAA / MX / TXT records; do not remove or reorder any existing apex record
- [x] 2.2 Run `go test -race -count=1 ./test/integration/... -run TestQuery_Apex_CNAME_TXT_Coexist` and confirm the test passes for all three sub-assertions

## 3. Regression verification — exact-match-first must hold

- [x] 3.1 Run `go test -race -count=1 ./test/integration/... -run 'TestQuery_(A|AAAA|MX|NS|TXT|SOA)$'` and confirm every test still passes; this validates that the dns-server requirement "Synthesize CNAME response when qtype does not match but CNAME exists at the queried name" continues to honor exact-match-first semantics in the presence of an apex CNAME, including the new "static zone record at the same owner as a CNAME wins over CNAME synthesis" scenario added to the spec
- [x] 3.2 Run `make test` and confirm the full unit + integration suite is green with 0 failures and 0 race-detector reports

## 4. Lint and spec validation

- [x] 4.1 [P] Run `make lint` and confirm no new golangci-lint findings introduced by the changes to `test/integration/query_test.go`, `testdata/integration/master/example.com_view-other.fwd`, and `testdata/integration/master/example.com_view-th.fwd`
- [x] 4.2 [P] Run `spectra validate apex-cname-txt-coexist` and confirm the change validates cleanly
