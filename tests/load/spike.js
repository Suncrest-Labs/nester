import exec from 'k6/execution';
import { endpointTargets, readThresholds, requestFor, websocket } from './common.js';
const scenarios = Object.fromEntries(Object.entries(endpointTargets).map(([name, rate]) => [name, { executor: 'ramping-arrival-rate', startRate: rate / 2, timeUnit: '1s', preAllocatedVUs: Math.max(20, rate), maxVUs: Math.max(100, rate * 8), stages: [{ target: rate / 2, duration: '30s' }, { target: rate * 5, duration: '1s' }, { target: rate * 5, duration: '30s' }, { target: rate / 2, duration: '1s' }, { target: rate / 2, duration: '30s' }] }]));
scenarios.websocket = { executor: 'ramping-vus', startVUs: 500, stages: [{ target: 500, duration: '30s' }, { target: 5000, duration: '1s' }, { target: 5000, duration: '30s' }, { target: 500, duration: '1s' }, { target: 500, duration: '30s' }], gracefulRampDown: '10s' };
export const options = { scenarios, thresholds: readThresholds, tags: { test_type: 'spike' } };
export default function () { return exec.scenario.name === 'websocket' ? websocket() : requestFor(exec.scenario.name); }
