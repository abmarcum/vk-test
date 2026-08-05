// --- Config constants (Tier 2) ---
// KNOWN DRIFT RISK (tracked in tech-debt register, §9.1):
// this value is a static guess matching a commonly-deployed default and
// MUST be reconciled with the server's actual configured max upload size.
// Do not treat this as a security boundary — server-side validation is authoritative.
const MAX_UPLOAD_SIZE_MB = 500;

const ACCEPTED_CONTENT_TYPES = ['video/mp4', 'video/webm', 'video/quicktime'];

// Content-type -> display badge label mapping (tracked in §9.3).
// Any content type accepted by the server but not listed here should
// fall back to a generic "VIDEO" badge rather than throwing.
const CONTENT_TYPE_BADGE_MAP = {
  'video/mp4': 'MP4',
  'video/webm': 'WEBM',
  'video/quicktime': 'MOV',
};

function badgeForContentType(contentType) {
  return CONTENT_TYPE_BADGE_MAP[contentType] || 'VIDEO';
}

// --- Upload with progress (XHR required — fetch upload-progress support
// is inconsistent across browsers as of this writing) ---
function uploadVideo(file, { onProgress, onSuccess, onError }) {
  const xhr = new XMLHttpRequest();
  const formData = new FormData();
  formData.append('file', file);

  xhr.upload.addEventListener('progress', (e) => {
    if (e.lengthComputable) {
      onProgress(Math.round((e.loaded / e.total) * 100));
    }
  });

  xhr.addEventListener('load', () => {
    if (xhr.status >= 200 && xhr.status < 300) {
      onSuccess(JSON.parse(xhr.responseText));
    } else {
      onError(xhr.status, xhr.responseText);
    }
  });

  xhr.addEventListener('error', () => onError(0, 'Network error'));

  xhr.open('POST', '/videos');
  xhr.send(formData);
}

// --- Pagination fetch logic (Tier 2) ---
// PENDING API CONFIRMATION (tracked as OQ-2, §14). Both variants below are
// stubbed so whichever contract Engineering confirms can be enabled by
// deleting the other branch — no redesign required either way.
function buildVideosQuery(paginationState) {
  if (paginationState.mode === 'cursor') {
    return paginationState.nextCursor
      ? `?cursor=${encodeURIComponent(paginationState.nextCursor)}`
      : '';
  }
  if (paginationState.mode === 'offset') {
    return `?limit=${paginationState.limit}&offset=${paginationState.offset}`;
  }
  throw new Error('Unresolved pagination mode — see OQ-2 in design spec §14.');
}
