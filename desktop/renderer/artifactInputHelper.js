// Artifact-input helper centralizes renderer-side access to the shared latest
// crawl artifact resolver. Organizer and subscription should both use this
// helper so fallback wording and empty-state handling stay identical.
//
// It should stay a pure renderer convenience layer and not learn artifact file
// parsing rules that belong to the Go bridge/services.
//
// Ownership summary:
// 1) centralize renderer-side access to the latest crawl artifact resolver
// 2) keep organizer/subscription fallback artifact input shaping consistent
// 3) separate renderer convenience access from artifact parsing logic
//
// File map for maintainers:
// 1) shared artifact resolver lookup
// 2) empty/latest/current artifact input helpers
// 3) renderer-facing fallback description helpers
(function initializeArtifactInputHelper(globalScope) {
  function getArtifactInputResolver(globalScopeRef) {
    const platformBridge = globalScopeRef.desktopPlatformBridge || null;
    return platformBridge && typeof platformBridge.getArtifactInputResolver === 'function'
      ? platformBridge.getArtifactInputResolver()
      : globalScopeRef.desktopArtifactInputResolver || null;
  }

  function createArtifactInputHelper(options = {}) {
    const resolver = options.resolver || getArtifactInputResolver(globalScope) || null;
    const readCurrentValue =
      typeof options.readCurrentValue === 'function' ? options.readCurrentValue : () => '';
    const writeCurrentValue =
      typeof options.writeCurrentValue === 'function' ? options.writeCurrentValue : (value) => String(value || '').trim();
    const labels = options.labels && typeof options.labels === 'object' ? options.labels : {};
    const latestInputOptions =
      options.latestInputOptions && typeof options.latestInputOptions === 'object' ? options.latestInputOptions : {};

    function emptyResolvedInput() {
      // Empty-shape fallback stays here so organizer/subscription callers share
      // one stable artifact contract even when no resolver is currently wired.
      return resolver && typeof resolver.emptyResolvedInput === 'function'
        ? resolver.emptyResolvedInput()
        : {
            value: '',
            type: '',
            sourceKind: '',
            sourceOrigin: '',
            isFallbackOutputDir: false
          };
    }

    async function resolveLatestInput(desktopApi, queryOptions = null) {
      // Renderer callers should always reuse the shared resolver contract here
      // instead of rebuilding panel/result/run-context fallback order locally.
      const normalizedQueryOptions =
        queryOptions && typeof queryOptions === 'object' ? queryOptions : latestInputOptions;
      if (resolver && typeof resolver.resolveLatestInputSafe === 'function') {
        return resolver.resolveLatestInputSafe(desktopApi, normalizedQueryOptions);
      }
      if (resolver && typeof resolver.resolveLatestInput === 'function') {
        return resolver.resolveLatestInput(desktopApi, normalizedQueryOptions);
      }
      return emptyResolvedInput();
    }

    function describeResolvedInput(action, latestArtifact, labels = {}, fallbackValue = '') {
      // Description wording belongs with the resolver contract so organizer and
      // subscription flows explain the same fallback source in the same way.
      if (resolver && typeof resolver.describeResolvedInputOrFallback === 'function') {
        return resolver.describeResolvedInputOrFallback(action, latestArtifact, labels, fallbackValue);
      }
      if (resolver && typeof resolver.describeResolvedInput === 'function') {
        return resolver.describeResolvedInput(action, latestArtifact, labels);
      }
      return '';
    }

    function getLabels() {
      return {
        snapshot: String(labels.snapshot || '快照').trim(),
        fallback: String(labels.fallback || '抓取结果输入').trim()
      };
    }

    function getCurrentArtifactInput() {
      return String(readCurrentValue() || '').trim();
    }

    function applyArtifactInputValue(artifactInput) {
      return String(writeCurrentValue(artifactInput) || '').trim();
    }

    function describeArtifactInput(action, latestArtifact, fallbackValue) {
      const scopedLabels = getLabels();
      const message = describeResolvedInput(action, latestArtifact, scopedLabels, fallbackValue);
      if (message) {
        return message;
      }
      const verb = action === 'filled' ? '已回填最近' : '自动回填最近';
      return fallbackValue ? `${verb}${scopedLabels.fallback}：${fallbackValue}` : '';
    }

    // This helper is the shared renderer-side textbox + latest-artifact
    // resolution boundary. Organizer and subscription should both reuse it
    // instead of each maintaining their own current-value/autofill flow.
    async function resolveArtifactInputState(desktopApi, queryOptions = null, stateOptions = {}) {
      const mode = String(stateOptions.mode || 'current').trim();
      const currentValue = getCurrentArtifactInput();
      if (currentValue) {
        return {
          artifactInput: currentValue,
          latestArtifact: null,
          message: ''
        };
      }

      if (mode !== 'autofill') {
        return {
          artifactInput: '',
          latestArtifact: null,
          message: ''
        };
      }

      const latestArtifact = await resolveLatestInput(desktopApi, queryOptions);
      const artifactInput = applyArtifactInputValue(latestArtifact && latestArtifact.value);
      return {
        artifactInput,
        latestArtifact,
        message: artifactInput ? describeArtifactInput('autofill', latestArtifact, artifactInput) : ''
      };
    }

    async function fillLatestArtifactInput(desktopApi) {
      const latestArtifact = await resolveLatestInput(desktopApi);
      const artifactInput = applyArtifactInputValue(latestArtifact && latestArtifact.value);
      return {
        artifactInput,
        latestArtifact,
        message: artifactInput ? describeArtifactInput('filled', latestArtifact, artifactInput) : ''
      };
    }

    return {
      applyArtifactInputValue,
      describeArtifactInput,
      describeResolvedInput,
      emptyResolvedInput,
      fillLatestArtifactInput,
      getCurrentArtifactInput,
      resolveArtifactInputState,
      resolveLatestInput
    };
  }

  globalScope.desktopArtifactInputHelper = {
    createArtifactInputHelper,
    getArtifactInputResolver: () => getArtifactInputResolver(globalScope)
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
