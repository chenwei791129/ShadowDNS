## ADDED Requirements

### Requirement: Reject a root zone with no apex SOA at load

State build SHALL treat a zone classified as root that has no apex SOA record as a load error, so that the invalid zone never becomes servable. On initial startup the load error SHALL abort startup with a message identifying the offending zone origin. On a SIGHUP reload the load error SHALL cause the reload to fail and the previously running state to be retained, consistent with the existing fail-soft reload model.

#### Scenario: SOA-less root zone aborts startup

- **WHEN** the server starts and a zone file classified as a root zone contains records but no apex SOA
- **THEN** startup fails with an error naming the offending zone origin, and no listener begins serving that zone

#### Scenario: SOA-less root zone introduced at reload retains prior state

- **WHEN** a SIGHUP reload encounters a root zone that now lacks an apex SOA
- **THEN** the reload fails with an error and the server continues serving the previously loaded state
