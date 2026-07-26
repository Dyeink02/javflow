export interface IndexValidationSampleProgressInput {
  previousBestCount: number;
  mergedCount: number;
  bestDiagnosticReason: string;
  sampleDiagnosticReason: string;
}

export interface IndexValidationSampleProgress {
  previousBestCount: number;
  mergedCount: number;
  currentBestCount: number;
  shouldPromoteMergedLinks: boolean;
  bestDiagnosticReason: string;
  effectiveDiagnosticReason: string;
}

export function resolveIndexValidationEffectiveDiagnosticReason(
  bestDiagnosticReason: string,
  fallbackDiagnosticReason: string
): string {
  return String(bestDiagnosticReason || fallbackDiagnosticReason || '');
}

export function resolveIndexValidationSampleProgress(
  input: IndexValidationSampleProgressInput
): IndexValidationSampleProgress {
  const shouldPromoteMergedLinks = input.mergedCount > input.previousBestCount;
  let bestDiagnosticReason = String(input.bestDiagnosticReason || '');
  const sampleDiagnosticReason = String(input.sampleDiagnosticReason || '');

  if (shouldPromoteMergedLinks) {
    if (sampleDiagnosticReason) {
      bestDiagnosticReason = sampleDiagnosticReason;
    }
  } else if (!bestDiagnosticReason && sampleDiagnosticReason) {
    bestDiagnosticReason = sampleDiagnosticReason;
  }

  return {
    previousBestCount: input.previousBestCount,
    mergedCount: input.mergedCount,
    currentBestCount: shouldPromoteMergedLinks ? input.mergedCount : input.previousBestCount,
    shouldPromoteMergedLinks,
    bestDiagnosticReason,
    effectiveDiagnosticReason: resolveIndexValidationEffectiveDiagnosticReason(
      bestDiagnosticReason,
      sampleDiagnosticReason
    )
  };
}
