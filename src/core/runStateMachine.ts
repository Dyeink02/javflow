export interface RunStateMachineState<TPhase extends string> {
  key: TPhase;
  label: string;
  execute: () => Promise<void>;
  next?: TPhase | null | (() => TPhase | null);
  shouldSkip?: () => boolean;
}

export interface RunStateMachineTransition<TPhase extends string> {
  from: TPhase | 'idle' | 'finished';
  to: TPhase | 'finished';
  label: string;
  skipped: boolean;
}

export interface RunStateMachineOptions<TPhase extends string> {
  initialState: TPhase;
  states: RunStateMachineState<TPhase>[];
  onTransition?: (transition: RunStateMachineTransition<TPhase>) => Promise<void> | void;
  onFinished?: () => Promise<void> | void;
}

class RunStateMachine<TPhase extends string> {
  private readonly statesByKey = new Map<TPhase, RunStateMachineState<TPhase>>();
  private readonly orderedKeys: TPhase[];
  private currentState: TPhase | 'idle' | 'finished' = 'idle';

  constructor(private readonly options: RunStateMachineOptions<TPhase>) {
    this.orderedKeys = options.states.map((state) => state.key);

    for (const state of options.states) {
      this.statesByKey.set(state.key, state);
    }
  }

  public getCurrentState(): TPhase | 'idle' | 'finished' {
    return this.currentState;
  }

  public async run(): Promise<void> {
    let nextStateKey: TPhase | null = this.options.initialState;
    let previousState: TPhase | 'idle' | 'finished' = 'idle';

    while (nextStateKey) {
      const state = this.requireState(nextStateKey);
      const skipped = state.shouldSkip?.() ?? false;

      this.currentState = state.key;
      if (this.options.onTransition) {
        await this.options.onTransition({
          from: previousState,
          to: state.key,
          label: state.label,
          skipped
        });
      }

      if (!skipped) {
        await state.execute();
      }

      previousState = state.key;
      nextStateKey = this.resolveNextState(state);
    }

    this.currentState = 'finished';
    if (this.options.onTransition) {
      await this.options.onTransition({
        from: previousState,
        to: 'finished',
        label: 'finished',
        skipped: false
      });
    }

    if (this.options.onFinished) {
      await this.options.onFinished();
    }
  }

  private resolveNextState(state: RunStateMachineState<TPhase>): TPhase | null {
    if (typeof state.next === 'function') {
      return state.next();
    }

    if (state.next !== undefined) {
      return state.next;
    }

    const currentIndex = this.orderedKeys.indexOf(state.key);
    if (currentIndex === -1 || currentIndex >= this.orderedKeys.length - 1) {
      return null;
    }

    return this.orderedKeys[currentIndex + 1];
  }

  private requireState(key: TPhase): RunStateMachineState<TPhase> {
    const state = this.statesByKey.get(key);
    if (!state) {
      throw new Error(`Unknown run state: ${String(key)}`);
    }

    return state;
  }
}

export default RunStateMachine;
