# Self-Update Mechanism via GitHub Releases

We will implement an automated version check and in-place binary update mechanism pointing to official releases at `https://github.com/herliansyah/cloudgate`.

A single static Go binary distributed without traditional package managers needs an effortless mechanism for users to obtain security patches and provider API updates. Checking the GitHub Releases API and performing an atomic file replacement of the running executable provides a seamless update experience both from the CLI and the Web UI.
