StreamGrab companion for Windows (unsigned, current user only).

1. Run install.ps1. It does not ask for an administrator password.
2. Install yt-dlp, FFmpeg, and Deno 2.3 or newer if the script says any of them are missing. This package does not include those programs. yt-dlp can update itself after the host is running.
3. Load the StreamGrab extension, quit the browser completely, then open a YouTube video.

Chrome loads the unpacked extension from chrome://extensions.
Firefox loads it from about:debugging, then This Firefox.

uninstall.ps1 removes the host program and the browser registration. It does not delete downloaded videos.
