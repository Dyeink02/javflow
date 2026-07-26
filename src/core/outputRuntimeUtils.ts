import fs from 'fs';
import path from 'path';

export interface OutputDirectoryResolution {
  baseOutputDir: string;
  outputDir: string;
  createdRunDir: boolean;
  reusedExistingDir: boolean;
  hasExistingArtifacts: boolean;
  reason:
    | 'resume-existing'
    | 'base-dir-empty'
    | 'base-dir-missing'
    | 'isolated-existing-output';
}

const CORE_OUTPUT_ARTIFACTS = ['filmData.json', 'magnet-links.txt', '未完成番号.txt'];

function padNumber(value: number): string {
  return String(value).padStart(2, '0');
}

function formatRunStamp(value = new Date()): string {
  return `${value.getFullYear()}${padNumber(value.getMonth() + 1)}${padNumber(value.getDate())}-${padNumber(
    value.getHours()
  )}${padNumber(value.getMinutes())}${padNumber(value.getSeconds())}`;
}

function hasHistoricalArtifacts(outputDir: string): boolean {
  const artifactPaths = [
    ...CORE_OUTPUT_ARTIFACTS.map((fileName) => path.join(outputDir, fileName)),
    path.join(outputDir, 'logs')
  ];

  return artifactPaths.some((targetPath) => fs.existsSync(targetPath));
}

export function resolveRunOutputDirectory(params: {
  outputDir: string;
  resumeExisting?: boolean;
  now?: Date;
}): OutputDirectoryResolution {
  const baseOutputDir = path.resolve(params.outputDir || process.cwd());

  if (params.resumeExisting) {
    return {
      baseOutputDir,
      outputDir: baseOutputDir,
      createdRunDir: false,
      reusedExistingDir: true,
      hasExistingArtifacts: false,
      reason: 'resume-existing'
    };
  }

  if (!fs.existsSync(baseOutputDir)) {
    return {
      baseOutputDir,
      outputDir: baseOutputDir,
      createdRunDir: false,
      reusedExistingDir: true,
      hasExistingArtifacts: false,
      reason: 'base-dir-missing'
    };
  }

  if (!hasHistoricalArtifacts(baseOutputDir)) {
    return {
      baseOutputDir,
      outputDir: baseOutputDir,
      createdRunDir: false,
      reusedExistingDir: true,
      hasExistingArtifacts: false,
      reason: 'base-dir-empty'
    };
  }

  const stamp = formatRunStamp(params.now);
  let suffix = 0;
  let candidate = path.join(baseOutputDir, `run-${stamp}`);

  while (fs.existsSync(candidate)) {
    suffix += 1;
    candidate = path.join(baseOutputDir, `run-${stamp}-${suffix}`);
  }

  return {
    baseOutputDir,
    outputDir: candidate,
    createdRunDir: true,
    reusedExistingDir: false,
    hasExistingArtifacts: true,
    reason: 'isolated-existing-output'
  };
}
