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

export async function listCheckRunsForHead({ token, owner, repo, headSha }) {
  const payload = await githubRequest({
    token,
    url: buildApiUrl(`/repos/${owner}/${repo}/commits/${headSha}/check-runs`, {
      per_page: 100,
    }),
  });
  return Array.isArray(payload?.check_runs) ? payload.check_runs : [];
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
