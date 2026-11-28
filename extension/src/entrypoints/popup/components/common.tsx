export function Blocked({ error, onUpdate }: { error: { code: string; message: string }; onUpdate: () => void }) {
  return (
    <div class="blocked">
      <strong>{labelFor(error.code)}</strong>
      <p>{error.message}</p>
      {error.code === "EXTRACTION_FAILED" && (
        <button type="button" class="get" onClick={onUpdate}>Update core</button>
      )}
    </div>
  );
}

export function Empty({ text }: { text: string }) {
  return <p class="empty">{text}</p>;
}

export function Skeleton() {
  return <div class="skeleton" aria-hidden="true" />;
}

function labelFor(code: string): string {
  switch (code) {
    case "BOT_CHECK":
      return "Bot check";
    case "AGE_RESTRICTED":
      return "Age-restricted";
    case "DRM_PROTECTED":
      return "DRM-protected";
    case "LIVE_STREAM":
      return "Live";
    case "DISK_FULL":
      return "Disk full";
    case "MUX_FAILED":
      return "Mux failed";
    case "HOST_INCOMPATIBLE":
      return "Update companion";
    default:
      return "Couldn't read video";
  }
}

