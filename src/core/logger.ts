import fs from 'fs';
import path from 'path';
import chalk from 'chalk';
import { createLogger, format, transports, Logger } from 'winston';

declare module 'winston' {
  interface Logger {
    success: (message: string) => Logger;
    progress: (message: string) => Logger;
    network: (message: string) => Logger;
    debugDetailed: (message: string, data?: unknown) => Logger;
  }
}

export interface LogEntry {
  level: string;
  message: string;
  timestamp: string;
}

type LogListener = (entry: LogEntry) => void;

const logListeners = new Set<LogListener>();

function stripAnsi(value: string): string {
  return value.replace(/\u001b\[[0-9;]*m/g, '');
}

function emitToListeners(level: string, message: string, timestamp: string): void {
  const entry: LogEntry = {
    level,
    message: stripAnsi(message),
    timestamp
  };

  for (const listener of logListeners) {
    listener(entry);
  }
}

function getLogDirectory(): string {
  const homeDir =
    (process.platform === 'win32' ? process.env.USERPROFILE : process.env.HOME) || process.cwd();
  const directory = process.env.JAV_SCRAPY_LOG_DIR || path.join(homeDir, '.jav-scrapy', 'logs');

  fs.mkdirSync(directory, { recursive: true });
  return directory;
}

const logDirectory = getLogDirectory();

const logger = createLogger({
  level: 'debug',
  format: format.combine(
    format.timestamp({ format: 'YYYY-MM-DD HH:mm:ss' }),
    format((info) => {
      const timestamp = String(info.timestamp || new Date().toISOString());
      emitToListeners(info.level, String(info.message), timestamp);
      return info;
    })(),
    format.printf(({ timestamp, level, message }) => `[${timestamp}] ${level.toUpperCase()}: ${message}`)
  ),
  transports: [
    new transports.Console({
      format: format.combine(
        format.colorize(),
        format.timestamp({ format: 'YYYY-MM-DD HH:mm:ss' }),
        format.printf(({ timestamp, level, message }) => `[${timestamp}] ${level.toUpperCase()}: ${message}`)
      )
    }),
    new transports.File({ filename: path.join(logDirectory, 'error.log'), level: 'error' }),
    new transports.File({ filename: path.join(logDirectory, 'combined.log') }),
    new transports.File({ filename: path.join(logDirectory, 'debug.log'), level: 'debug' })
  ]
});

logger.success = (message: string) => logger.info(chalk.green(`OK ${message}`));
logger.progress = (message: string) => logger.info(chalk.blue(`PROGRESS ${message}`));
logger.network = (message: string) => logger.info(chalk.yellow(`NETWORK ${message}`));
logger.debugDetailed = (message: string, data?: unknown) => {
  if (typeof data !== 'undefined') {
    return logger.debug(`${message} | data: ${JSON.stringify(data, null, 2)}`);
  }

  return logger.debug(message);
};

export function subscribeToLogs(listener: LogListener): () => void {
  logListeners.add(listener);
  return () => {
    logListeners.delete(listener);
  };
}

export default logger;
