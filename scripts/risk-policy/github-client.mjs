#!/usr/bin/env node

function buildApiUrl(pathname, query = {}) {
  const url = new URL(`https://api.github.com${pathname}`);
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === "") continue;
    url.searchParams.set(key, String(value));
  }
  return url.toString();
}

async function githubRequest({ token, method = "GET", url, body }) {
  const response = await fetch(url, {
    method,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "X-GitHub-Api-Version": "2022-11-28",
      "User-Agent": "contribution-risk-policy",
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    ...(body ? { body: JSON.stringify(body) } : {}),
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(
      `GitHub API ${method} ${url} failed (${response.status}): ${text}`,
    );
  }

  if (response.status === 204) return null;
  return response.json();
}

async function listPaginated({ token, initialPath, query, key, stopWhen }) {
  const items = [];
  let page = 1;
  while (true) {
    const payload = await githubRequest({
      token,
      url: buildApiUrl(initialPath, { per_page: 100, page, ...query }),
    });
    const chunk = Array.isArray(payload?.[key]) ? payload[key] : payload;
    if (!Array.isArray(chunk) || chunk.length === 0) break;
    items.push(...chunk);
    if (typeof stopWhen === "function" && stopWhen(chunk)) break;
    if (chunk.length < 100) break;
    page += 1;
  }
  return items;
}

export async function getPullRequest({ token, owner, repo, pullNumber }) {
  return githubRequest({
    token,
    url: buildApiUrl(`/repos/${owner}/${repo}/pulls/${pullNumber}`),
  });
}

export async function getIssue({ token, owner, repo, issueNumber }) {
  return githubRequest({
    token,
    url: buildApiUrl(`/repos/${owner}/${repo}/issues/${issueNumber}`),
  });
}

export async function listPullRequestFiles({ token, owner, repo, pullNumber }) {
  return listPaginated({
    token,
    initialPath: `/repos/${owner}/${repo}/pulls/${pullNumber}/files`,
    query: {},
    key: null,
  });
}

export async function listIssueComments({ token, owner, repo, issueNumber }) {
  return listPaginated({
    token,
    initialPath: `/repos/${owner}/${repo}/issues/${issueNumber}/comments`,
    query: {},
    key: null,
  });
}

export async function createIssueComment({
  token,
  owner,
  repo,
  issueNumber,
  body,
}) {
  return githubRequest({
    token,
    method: "POST",
    url: buildApiUrl(`/repos/${owner}/${repo}/issues/${issueNumber}/comments`),
    body: { body },
  });
}

export async function updateIssue({
  token,
  owner,
  repo,
  issueNumber,
  body,
  labels,
  state,
}) {
  return githubRequest({
    token,
    method: "PATCH",
    url: buildApiUrl(`/repos/${owner}/${repo}/issues/${issueNumber}`),
    body: {
      ...(body !== undefined ? { body } : {}),
      ...(labels ? { labels } : {}),
      ...(state ? { state } : {}),
    },
  });
}

export async function createIssue({ token, owner, repo, title, body, labels }) {
  return githubRequest({
    token,
    method: "POST",
    url: buildApiUrl(`/repos/${owner}/${repo}/issues`),
    body: { title, body, ...(labels ? { labels } : {}) },
  });
}

export async function listRepoIssues({
  token,
  owner,
  repo,
  state = "open",
  labels,
}) {
  return listPaginated({
    token,
    initialPath: `/repos/${owner}/${repo}/issues`,
    query: {
      state,
      ...(labels ? { labels } : {}),
    },
    key: null,
  });
}

export async function addIssueLabels({
  token,
  owner,
  repo,
  issueNumber,
  labels,
}) {
  return githubRequest({
    token,
    method: "POST",
    url: buildApiUrl(`/repos/${owner}/${repo}/issues/${issueNumber}/labels`),
    body: { labels },
  });
}

export async function listCheckRunsForHead({ token, owner, repo, headSha }) {
  const payload = await githubRequest({
    token,
    url: buildApiUrl(`/repos/${owner}/${repo}/commits/${headSha}/check-runs`, {
      per_page: 100,
    }),
  });
  return Array.isArray(payload?.check_runs) ? payload.check_runs : [];
}

export async function listWorkflowRuns({
  token,
  owner,
  repo,
  workflowId,
  event,
  status,
  stopWhen,
}) {
  return listPaginated({
    token,
    initialPath: `/repos/${owner}/${repo}/actions/workflows/${workflowId}/runs`,
    query: { event, status },
    key: "workflow_runs",
    stopWhen,
  });
}

export async function listWorkflowRunJobs({ token, owner, repo, runId }) {
  return listPaginated({
    token,
    initialPath: `/repos/${owner}/${repo}/actions/runs/${runId}/jobs`,
    query: {},
    key: "jobs",
  });
}

export async function listWorkflowRunArtifacts({ token, owner, repo, runId }) {
  return listPaginated({
    token,
    initialPath: `/repos/${owner}/${repo}/actions/runs/${runId}/artifacts`,
    query: {},
    key: "artifacts",
  });
}

export async function downloadArtifactArchive({ token, archiveUrl }) {
  const response = await fetch(archiveUrl, {
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "X-GitHub-Api-Version": "2022-11-28",
      "User-Agent": "contribution-risk-policy",
    },
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(
      `GitHub artifact download failed (${response.status}) for ${archiveUrl}: ${text}`,
    );
  }

  const arrayBuffer = await response.arrayBuffer();
  return Buffer.from(arrayBuffer);
}

export async function graphqlRequest({ token, query, variables }) {
  const payload = await githubRequest({
    token,
    method: "POST",
    url: "https://api.github.com/graphql",
    body: { query, variables },
  });
  if (Array.isArray(payload?.errors) && payload.errors.length > 0) {
    const message = payload.errors
      .map((entry) => String(entry?.message ?? "GraphQL error"))
      .join("; ");
    throw new Error(`GitHub GraphQL request failed: ${message}`);
  }
  return payload;
}
