((root) => {
  const DEFAULT_TIMEOUT_MS = 5_000;
  const DEFAULT_INTERVAL_MS = 50;

  class LibraryResolutionError extends Error {
    constructor(reason) {
      super(reason);
      this.name = "LibraryResolutionError";
      this.safeNoSend = true;
    }
  }

  function normalizeFilename(value) {
    return String(value ?? "").normalize("NFC").trim();
  }

  function normalizeRequestedFiles(files) {
    if (!Array.isArray(files) || files.length === 0 || files.length > 16) {
      throw new LibraryResolutionError("attachment_profile_invalid");
    }
    const normalized = files.map(normalizeFilename);
    if (normalized.some((file) => !file || file.length > 200 || file.includes("/") || file.includes("\\"))) {
      throw new LibraryResolutionError("attachment_filename_invalid");
    }
    if (new Set(normalized).size !== normalized.length) {
      throw new LibraryResolutionError("attachment_profile_duplicate");
    }
    return normalized;
  }

  function exactMatches(nodes, filename, readName) {
    return [...nodes].filter((node) => normalizeFilename(readName(node)) === filename);
  }

  async function waitFor(read, predicate, hooks, timeoutReason) {
    const now = hooks.now || (() => Date.now());
    const delay = hooks.delay || ((milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)));
    const timeout = hooks.timeoutMs || DEFAULT_TIMEOUT_MS;
    const interval = hooks.intervalMs || DEFAULT_INTERVAL_MS;
    const deadline = now() + timeout;
    while (now() < deadline) {
      const value = read();
      if (predicate(value)) return value;
      await delay(interval);
    }
    throw new LibraryResolutionError(timeoutReason);
  }

  async function resolveExactAttachments(files, hooks) {
    if (!hooks || typeof hooks.setQuery !== "function" || typeof hooks.optionNodes !== "function"
      || typeof hooks.optionName !== "function" || typeof hooks.clickOption !== "function"
      || typeof hooks.chipNodes !== "function" || typeof hooks.chipName !== "function") {
      throw new LibraryResolutionError("library_resolver_hooks_invalid");
    }
    const requested = normalizeRequestedFiles(files);
    for (const filename of requested) {
      await hooks.setQuery(`@${filename}`);
      const options = await waitFor(
        hooks.optionNodes,
        (nodes) => exactMatches(nodes, filename, hooks.optionName).length > 0,
        hooks,
        "attachment_exact_match_missing",
      );
      const matches = exactMatches(options, filename, hooks.optionName);
      if (matches.length !== 1) {
        throw new LibraryResolutionError(matches.length === 0 ? "attachment_exact_match_missing" : "attachment_exact_match_ambiguous");
      }
      await hooks.clickOption(matches[0]);
      await waitFor(
        hooks.chipNodes,
        (nodes) => exactMatches(nodes, filename, hooks.chipName).length === 1,
        hooks,
        "attachment_chip_missing",
      );
    }
    const evidence = [...hooks.chipNodes()].map((node) => normalizeFilename(hooks.chipName(node))).filter(Boolean);
    if (evidence.length !== requested.length || evidence.some((file, index) => file !== requested[index])) {
      throw new LibraryResolutionError("attachment_chip_order_mismatch");
    }
    return evidence;
  }

  root.CDDMLibraryResolver = Object.freeze({
    LibraryResolutionError,
    normalizeFilename,
    normalizeRequestedFiles,
    resolveExactAttachments,
  });
})(globalThis);
