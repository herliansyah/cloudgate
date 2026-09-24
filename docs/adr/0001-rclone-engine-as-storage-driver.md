# Embed Rclone Engine as Core Storage Driver

We will embed the open-source `rclone/fs` engine packages directly inside the single Go binary as our cloud storage abstraction layer instead of writing custom SDK connectors for each provider.

Connecting to 10+ cloud storage providers requires handling vendor-specific OAuth2 flows, token renewals, chunked uploads, exponential backoffs, and rate-limiting to comply with provider Terms of Service (TOS). Embedding `rclone/fs` provides battle-tested driver implementations for over 40 providers while keeping the deployment as a zero-dependency, single Go binary without requiring an external rclone executable installed on the host.
