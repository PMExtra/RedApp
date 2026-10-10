(() => {
  // The sandboxed document has an opaque origin; the embedding page sizes the
  // frame from these messages instead of reading the document.
  if (window.parent === window) return;
  let sent = -1;
  const post = () => {
    const height = Math.ceil(document.body.scrollHeight);
    if (height === sent) return;
    sent = height;
    window.parent.postMessage({ type: 'redapp-instructions-height', height }, location.origin);
  };
  if (typeof ResizeObserver === 'function') new ResizeObserver(post).observe(document.body);
  addEventListener('load', post);
  post();
})();
