// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

use tonic_health::ServingStatus;
use tonic_health::server::HealthReporter;
use types::cluster_agent::{log_metadata_service_server, log_records_service_server};

/// The services the health service reports on. "" is the server as a whole.
const SERVICES: [&str; 3] = [
    "",
    log_metadata_service_server::SERVICE_NAME,
    log_records_service_server::SERVICE_NAME,
];

/// Reports every service as serving.
pub async fn set_serving(reporter: &HealthReporter) {
    set_status(reporter, ServingStatus::Serving).await;
}

/// Reports every service as not serving.
pub async fn set_not_serving(reporter: &HealthReporter) {
    set_status(reporter, ServingStatus::NotServing).await;
}

async fn set_status(reporter: &HealthReporter, status: ServingStatus) {
    for name in SERVICES {
        reporter.set_service_status(name, status).await;
    }
}

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used)]

    use tonic::Request;
    use tonic_health::pb::HealthCheckRequest;
    use tonic_health::pb::health_check_response::ServingStatus as WireStatus;
    use tonic_health::pb::health_server::Health;
    use tonic_health::server::HealthService;

    use super::*;

    async fn check(reporter: &HealthReporter, service: &str) -> WireStatus {
        let health = HealthService::from_health_reporter(reporter.clone());
        let request = Request::new(HealthCheckRequest {
            service: service.to_owned(),
        });
        let response = health.check(request).await.unwrap();
        response.into_inner().status()
    }

    #[tokio::test]
    async fn set_serving_reports_every_service_serving() {
        let reporter = HealthReporter::new();
        set_serving(&reporter).await;

        for name in SERVICES {
            assert_eq!(check(&reporter, name).await, WireStatus::Serving, "{name}");
        }
    }

    #[tokio::test]
    async fn set_not_serving_reports_every_service_not_serving() {
        let reporter = HealthReporter::new();
        set_serving(&reporter).await;
        set_not_serving(&reporter).await;

        for name in SERVICES {
            assert_eq!(
                check(&reporter, name).await,
                WireStatus::NotServing,
                "{name}"
            );
        }
    }
}
