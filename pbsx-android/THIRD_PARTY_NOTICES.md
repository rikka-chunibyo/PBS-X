The Android application follows the repository's AGPL-3.0-or-later license
described in [debian/copyright](../debian/copyright).

The APK includes [the complete third-party notices](app/src/main/assets/THIRD_PARTY_NOTICES.txt):

- Go standard library and Go mobile bindings: BSD 3-Clause and Go patent grant.
- golang.org/x/net, x/sys, x/crypto and x/text: BSD 3-Clause and Go patent grant.
- github.com/klauspost/compress: BSD 3-Clause, with its upstream third-party notices.
- github.com/pbs-plus/pxar: MIT, with local PBS-X changes.
- dev.rikka.shizuku API/provider/shared/aidl 13.1.5: MIT, RikkaW.

AndroidX Test and JUnit are used by the separate test APK and unit tests,
respectively. They are not application runtime dependencies. Gradle packages
their corresponding notices with the test artifacts. The Google Java formatter
is a development tool and is not shipped in the APK.
