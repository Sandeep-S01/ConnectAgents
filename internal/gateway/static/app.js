(function () {
  var state = {
    projects: [],
    tasks: [],
    selectedProjectId: "",
    selectedTaskId: "",
    eventSource: null,
    isEditingProject: false
  };

  var nodes = {};

  function $(id) {
    return document.getElementById(id);
  }

  function init() {
    nodes.tokenForm = $("token-form");
    nodes.tokenInput = $("auth-token");
    nodes.projectForm = $("project-form");
    nodes.taskForm = $("task-form");
    nodes.projectSelect = $("project-select");
    nodes.approvalList = $("approval-list");
    nodes.taskList = $("task-list");
    nodes.eventLog = $("event-log");
    nodes.notice = $("notice");
    nodes.taskDetail = $("task-detail");
    nodes.taskStatus = $("task-status");
    nodes.projectCount = $("project-count");
    nodes.approvalCount = $("approval-count");
    nodes.projectSubmitBtn = $("project-submit-btn");
    nodes.projectCancelEdit = $("project-cancel-edit");
    nodes.projectDetails = $("project-details");
    nodes.projectDetailTech = $("project-detail-tech");
    nodes.projectEditBtn = $("project-edit-btn");
    nodes.projectDeleteBtn = $("project-delete-btn");
    nodes.projectGitMsgBtn = $("project-git-msg-btn");
    nodes.projectGitPushBtn = $("project-git-push-btn");
    nodes.projectGitCommitBox = $("project-git-commit-box");
    nodes.projectGitCommitMsg = $("project-git-commit-msg");
    nodes.projectGitCommitBtn = $("project-git-commit-btn");
    nodes.projectDetailTestCmd = $("project-detail-test-cmd");
    nodes.projectRunTestsBtn = $("project-run-tests-btn");
    nodes.projectTestResultBox = $("project-test-result-box");
    nodes.leftSidebar = $("left-sidebar");
    nodes.rightDrawer = $("right-drawer");
    nodes.btnToggleSidebar = $("btn-toggle-sidebar");
    nodes.sidebarCloseTrigger = $("sidebar-close-trigger");
    nodes.btnToggleDrawer = $("btn-toggle-drawer");
    nodes.drawerCloseTrigger = $("drawer-close-trigger");
    nodes.projectSearch = $("project-search");
    nodes.headerProjectName = $("header-project-name");
    nodes.headerAuthIndicator = $("header-auth-indicator");
    nodes.navApprovalBadge = $("approval-count-badge");
    nodes.taskHistoryCount = $("task-history-count");

    nodes.tokenForm.addEventListener("submit", login);
    nodes.projectForm.addEventListener("submit", onProjectFormSubmit);
    nodes.taskForm.addEventListener("submit", createTaskFlow);
    nodes.projectSelect.addEventListener("change", selectProject);
    nodes.projectCancelEdit.addEventListener("click", cancelEditProject);
    nodes.projectEditBtn.addEventListener("click", editProject);
    nodes.projectDeleteBtn.addEventListener("click", deleteProject);
    nodes.projectGitMsgBtn.addEventListener("click", generateCommitMsg);
    nodes.projectGitCommitBtn.addEventListener("click", gitCommit);
    nodes.projectGitPushBtn.addEventListener("click", gitPush);
    nodes.projectRunTestsBtn.addEventListener("click", runProjectTests);

    // Sidebar & Drawer Toggle events
    if (nodes.btnToggleSidebar) {
      nodes.btnToggleSidebar.addEventListener("click", function() {
        nodes.leftSidebar.classList.toggle("collapsed");
      });
    }
    if (nodes.sidebarCloseTrigger) {
      nodes.sidebarCloseTrigger.addEventListener("click", function() {
        nodes.leftSidebar.classList.add("collapsed");
      });
    }
    if (nodes.btnToggleDrawer) {
      nodes.btnToggleDrawer.addEventListener("click", function() {
        nodes.rightDrawer.classList.toggle("collapsed");
      });
    }
    if (nodes.drawerCloseTrigger) {
      nodes.drawerCloseTrigger.addEventListener("click", function() {
        nodes.rightDrawer.classList.add("collapsed");
      });
    }

    if (nodes.projectSearch) {
      nodes.projectSearch.addEventListener("input", filterProjects);
    }

    // Modal Opening
    var btnNewProject = $("btn-new-project");
    if (btnNewProject) {
      btnNewProject.addEventListener("click", openNewProjectModal);
    }
    var modalCloseBtn = $("modal-close-btn");
    if (modalCloseBtn) {
      modalCloseBtn.addEventListener("click", closeProjectModal);
    }

    // Close modal when clicking outside the card
    var modal = $("project-modal");
    if (modal) {
      modal.addEventListener("click", function(e) {
        if (e.target === modal) closeProjectModal();
      });
    }

    // View Navigation Handler
    document.querySelectorAll(".nav-item").forEach(function(item) {
      item.addEventListener("click", function() {
        document.querySelectorAll(".nav-item").forEach(function(el) { el.classList.remove("active"); });
        item.classList.add("active");

        var viewId = item.dataset.view;
        document.querySelectorAll(".view-pane").forEach(function(pane) {
          pane.classList.remove("active");
        });
        var targetPane = $(viewId);
        if (targetPane) {
          targetPane.classList.add("active");
        }
      });
    });

    // Chat suggestions click delegate & code copy
    var chatHistory = $("chat-history");
    if (chatHistory) {
      chatHistory.addEventListener("click", function(event) {
        if (event.target.classList.contains("btn-prompt-suggestion")) {
          var suggestion = event.target.dataset.prompt;
          var textarea = nodes.taskForm.querySelector("textarea");
          if (textarea) {
            textarea.value = suggestion;
          }
          if (!state.selectedProjectId) {
            showError(new Error("Please select or create a project first."));
            return;
          }
          createTask();
        }
        if (event.target.classList.contains("btn-copy-code")) {
          var code = event.target.closest(".code-window-card").querySelector("pre code").textContent;
          navigator.clipboard.writeText(code).then(function() {
            event.target.textContent = "Copied!";
            setTimeout(function() {
              event.target.textContent = "Copy";
            }, 2000);
          });
        }
      });
    }

    // Textarea submit on Enter (Shift+Enter = newline)
    var textarea = nodes.taskForm.querySelector("textarea");
    if (textarea) {
      textarea.addEventListener("keydown", function(event) {
        if (event.key === "Enter" && !event.shiftKey) {
          event.preventDefault();
          createTask();
        }
      });
    }

    // Safe element event binders (elements that might not exist if legacy)
    bindIfExists("refresh-tasks", "click", loadTasks);
    bindIfExists("refresh-approvals", "click", loadApprovals);
    bindIfExists("connect-events", "click", connectEvents);
    bindIfExists("logout", "click", logout);
    bindIfExists("openrouter-form", "submit", saveOpenRouterKey);
    bindIfExists("openrouter-clear-btn", "click", clearOpenRouterKey);
    bindIfExists("openai-form", "submit", saveOpenAIKey);
    bindIfExists("openai-clear-btn", "click", clearOpenAIKey);

    if ("serviceWorker" in navigator) {
      navigator.serviceWorker.register("/sw.js").catch(function () {});
    }
    loadProjects();
    loadApprovals();
    updateAuthStatusIndicator();
  }

  function bindIfExists(id, event, handler) {
    var el = $(id);
    if (el) el.addEventListener(event, handler);
  }

  // ── Project Modal ────────────────────────────────────────────────────────

  function openNewProjectModal() {
    state.isEditingProject = false;
    nodes.projectForm.reset();
    nodes.projectSubmitBtn.textContent = "Add Project";
    $("modal-project-title").textContent = "Add New Project";
    $("project-modal").style.display = "flex";
  }

  function closeProjectModal() {
    $("project-modal").style.display = "none";
  }

  // ── Project Form Submit (create OR update) ───────────────────────────────

  function onProjectFormSubmit(event) {
    event.preventDefault();
    if (state.isEditingProject) {
      updateProject();
    } else {
      createProject();
    }
  }

  function createProject() {
    var form = nodes.projectForm;
    var name = form.querySelector("input[name='name']").value.trim();
    var path = form.querySelector("input[name='path']").value.trim();
    var techStack = form.querySelector("input[name='techStack']").value.trim();
    var testCommand = form.querySelector("input[name='testCommand']").value.trim();

    if (!name || !path) {
      showError(new Error("Project Name and Path are required."));
      return;
    }

    api("/api/projects", {
      method: "POST",
      body: JSON.stringify({ name: name, path: path, techStack: techStack, testCommand: testCommand })
    }).then(function (project) {
      state.selectedProjectId = project.id;
      closeProjectModal();
      form.reset();
      notice("Project \"" + project.name + "\" created!");
      loadProjects();
    }).catch(showError);
  }

  function updateProject() {
    var form = nodes.projectForm;
    var name = form.querySelector("input[name='name']").value.trim();
    var path = form.querySelector("input[name='path']").value.trim();
    var techStack = form.querySelector("input[name='techStack']").value.trim();
    var testCommand = form.querySelector("input[name='testCommand']").value.trim();

    api("/api/projects/" + encodeURIComponent(state.selectedProjectId), {
      method: "PATCH",
      body: JSON.stringify({ name: name, path: path, techStack: techStack, testCommand: testCommand })
    }).then(function (project) {
      closeProjectModal();
      state.isEditingProject = false;
      notice("Project updated.");
      loadProjects();
    }).catch(showError);
  }

  // ── Search filter ────────────────────────────────────────────────────────

  function filterProjects() {
    var query = nodes.projectSearch.value.toLowerCase().trim();
    document.querySelectorAll(".project-card-mini").forEach(function(card) {
      var title = card.querySelector(".title").textContent.toLowerCase();
      var meta = card.querySelector(".meta").textContent.toLowerCase();
      card.style.display = (title.indexOf(query) !== -1 || meta.indexOf(query) !== -1) ? "block" : "none";
    });
  }

  // ── API helper ───────────────────────────────────────────────────────────

  function api(path, options) {
    options = options || {};
    options.credentials = "same-origin";
    var customKey = localStorage.getItem("openrouter_api_key") || "";
    var customModel = localStorage.getItem("openrouter_model") || "";
    var openaiKey = localStorage.getItem("openai_api_key") || "";
    var customHeaders = { "Content-Type": "application/json" };
    if (customKey) {
      customHeaders["X-OpenRouter-API-Key"] = customKey;
    }
    if (customModel) {
      customHeaders["X-OpenRouter-Model"] = customModel;
    }
    if (openaiKey) {
      customHeaders["X-OpenAI-API-Key"] = openaiKey;
    }
    options.headers = Object.assign(customHeaders, options.headers || {});
    return fetch(path, options).then(function (response) {
      return response.text().then(function (text) {
        var body = text ? JSON.parse(text) : {};
        if (!response.ok) {
          throw new Error(body.error || response.statusText);
        }
        return body;
      });
    });
  }

  // ── Auth ─────────────────────────────────────────────────────────────────

  function login(event) {
    event.preventDefault();
    api("/auth/login", {
      method: "POST",
      body: JSON.stringify({ token: nodes.tokenInput.value.trim() })
    }).then(function () {
      nodes.tokenInput.value = "";
      notice("Signed in.");
      loadProjects();
      loadApprovals();
      updateAuthStatusIndicator();
    }).catch(showError);
  }

  function logout() {
    api("/auth/logout", { method: "POST" }).then(function () {
      if (state.eventSource) {
        state.eventSource.close();
        state.eventSource = null;
      }
      notice("Signed out.");
      updateAuthStatusIndicator();
    }).catch(showError);
  }

  function updateAuthStatusIndicator() {
    var indicator = $("settings-auth-status");
    var isLoggedIn = document.cookie.indexOf("paia_token") !== -1;
    if (indicator) {
      indicator.textContent = isLoggedIn ? "Authenticated" : "Localhost Access";
    }
    // Update header auth badge
    if (nodes.headerAuthIndicator) {
      nodes.headerAuthIndicator.textContent = isLoggedIn ? "● Authenticated" : "● Local";
      nodes.headerAuthIndicator.style.color = isLoggedIn ? "var(--primary)" : "var(--muted)";
    }
    // Show/hide logout button
    var logoutBtn = $("logout");
    if (logoutBtn) {
      logoutBtn.style.display = isLoggedIn ? "flex" : "none";
    }
    updateOpenRouterStatusIndicator();
    updateOpenAIStatusIndicator();
  }

  function updateOpenRouterStatusIndicator() {
    var indicator = $("settings-openrouter-status");
    var hasKey = !!localStorage.getItem("openrouter_api_key");
    if (indicator) {
      indicator.textContent = hasKey ? "Configured" : "Not Configured";
      indicator.style.background = hasKey ? "rgba(16, 185, 129, 0.1)" : "rgba(100, 116, 139, 0.1)";
      indicator.style.color = hasKey ? "var(--primary)" : "var(--muted)";
      indicator.style.borderColor = hasKey ? "rgba(16, 185, 129, 0.2)" : "rgba(100, 116, 139, 0.2)";
    }
    var keyInput = $("openrouter-api-key");
    if (keyInput) {
      keyInput.value = hasKey ? "••••••••••••••••••••" : "";
    }
    var modelInput = $("openrouter-model");
    if (modelInput) {
      modelInput.value = localStorage.getItem("openrouter_model") || "";
    }
  }

  function saveOpenRouterKey(event) {
    if (event) event.preventDefault();
    var input = $("openrouter-api-key");
    var modelInput = $("openrouter-model");
    
    var key = input ? input.value.trim() : "";
    var model = modelInput ? modelInput.value.trim() : "";

    // Save key if updated and not masked
    if (key && key.indexOf("•••") !== 0) {
      localStorage.setItem("openrouter_api_key", key);
    } else if (!key && !localStorage.getItem("openrouter_api_key")) {
      showError(new Error("API Key is required to save configuration."));
      return;
    }
    
    // Save model name
    localStorage.setItem("openrouter_model", model);
    
    notice("OpenRouter config saved.");
    updateOpenRouterStatusIndicator();
  }

  function clearOpenRouterKey() {
    localStorage.removeItem("openrouter_api_key");
    localStorage.removeItem("openrouter_model");
    notice("OpenRouter config cleared.");
    updateOpenRouterStatusIndicator();
  }

  function updateOpenAIStatusIndicator() {
    var indicator = $("settings-openai-status");
    var hasKey = !!localStorage.getItem("openai_api_key");
    if (indicator) {
      indicator.textContent = hasKey ? "Configured" : "Not Configured";
      indicator.style.background = hasKey ? "rgba(16, 185, 129, 0.1)" : "rgba(100, 116, 139, 0.1)";
      indicator.style.color = hasKey ? "var(--primary)" : "var(--muted)";
      indicator.style.borderColor = hasKey ? "rgba(16, 185, 129, 0.2)" : "rgba(100, 116, 139, 0.2)";
    }
    var keyInput = $("openai-api-key");
    if (keyInput) {
      keyInput.value = hasKey ? "••••••••••••••••••••" : "";
    }
  }

  function saveOpenAIKey(event) {
    if (event) event.preventDefault();
    var input = $("openai-api-key");
    var key = input ? input.value.trim() : "";
    if (key && key.indexOf("•••") !== 0) {
      localStorage.setItem("openai_api_key", key);
    } else if (!key && !localStorage.getItem("openai_api_key")) {
      showError(new Error("OpenAI API Key is required to save configuration."));
      return;
    }
    notice("OpenAI config saved.");
    updateOpenAIStatusIndicator();
  }

  function clearOpenAIKey() {
    localStorage.removeItem("openai_api_key");
    notice("OpenAI config cleared.");
    updateOpenAIStatusIndicator();
  }

  // ── Projects ─────────────────────────────────────────────────────────────

  function loadProjects() {
    return api("/api/projects").then(function (projects) {
      state.projects = projects || [];
      nodes.projectCount.textContent = String(state.projects.length);

      // Sync legacy hidden select
      nodes.projectSelect.innerHTML = "";
      state.projects.forEach(function (project) {
        var option = document.createElement("option");
        option.value = project.id;
        option.textContent = project.name + " — " + project.path;
        nodes.projectSelect.appendChild(option);
      });

      // Auto-select first project if none selected
      if (!state.selectedProjectId && state.projects.length > 0) {
        state.selectedProjectId = state.projects[0].id;
        nodes.projectSelect.value = state.selectedProjectId;
      }

      // Rebuild sidebar mini cards
      var listContainer = $("project-list-container");
      if (listContainer) {
        listContainer.innerHTML = "";
        if (state.projects.length === 0) {
          listContainer.innerHTML = '<p style="font-size:12px;color:var(--muted);padding:8px 4px;">No projects yet. Click + New Project.</p>';
        } else {
          state.projects.forEach(function (project) {
            var card = document.createElement("button");
            card.type = "button";
            card.className = "project-card-mini" + (project.id === state.selectedProjectId ? " active" : "");
            card.innerHTML =
              '<div class="title">' + escapeHtml(project.name) + '</div>' +
              '<div class="meta">' + escapeHtml(project.path) + '</div>';
            card.addEventListener("click", function() {
              state.selectedProjectId = project.id;
              nodes.projectSelect.value = project.id;
              selectProject();
            });
            listContainer.appendChild(card);
          });
        }
      }

      // Update header & right drawer
      var project = state.projects.find(function(p) { return p.id === state.selectedProjectId; });
      if (project) {
        nodes.projectDetailTech.textContent = project.techStack || "None";
        nodes.projectDetailTestCmd.textContent = project.testCommand || "None";
        nodes.headerProjectName.textContent = project.name;
      } else {
        nodes.headerProjectName.textContent = "No project selected";
        nodes.projectDetailTech.textContent = "None";
        nodes.projectDetailTestCmd.textContent = "None";
      }

      loadTasks();
    }).catch(showError);
  }

  // ── Task creation ─────────────────────────────────────────────────────────

  function createTaskFlow(event) {
    event.preventDefault();
    createTask();
  }

  function createTask() {
    if (!state.selectedProjectId) {
      showError(new Error("Please select a project first."));
      return;
    }
    var textarea = nodes.taskForm.querySelector("textarea");
    var promptVal = textarea ? textarea.value.trim() : "";
    if (!promptVal) return;

    var agentSelect = nodes.taskForm.querySelector("select[name='agentType']");
    var agentType = agentSelect ? agentSelect.value : "codex";

    api("/api/tasks", {
      method: "POST",
      body: JSON.stringify({ projectId: state.selectedProjectId, agentType: agentType, prompt: promptVal })
    }).then(function (task) {
      state.selectedTaskId = task.id;
      if (textarea) textarea.value = "";
      loadTasks();
      connectEvents();
      // Auto-run immediately
      api("/api/tasks/" + task.id + "/run", { method: "POST" })
        .then(function () {
          notice("Agent started.");
          refreshTask();
          loadTasks();
        })
        .catch(showError);
    }).catch(showError);
  }

  // ── Tasks list ───────────────────────────────────────────────────────────

  function loadTasks() {
    if (!state.selectedProjectId) {
      state.tasks = [];
      renderTaskList([]);
      renderConversation([]);
      return Promise.resolve();
    }
    return api("/api/tasks?projectId=" + encodeURIComponent(state.selectedProjectId)).then(function (tasks) {
      state.tasks = tasks || [];
      renderTaskList(state.tasks);
      renderConversation(state.tasks);
      if (!state.selectedTaskId && state.tasks.length > 0) {
        selectTask(state.tasks[0].id);
      }
    }).catch(showError);
  }

  function renderTaskList(tasks) {
    nodes.taskList.innerHTML = "";
    if (!tasks || !tasks.length) {
      nodes.taskList.innerHTML = '<p class="empty" style="padding:16px;color:var(--muted);">No tasks for this project.</p>';
      return;
    }
    tasks.forEach(function (task) {
      var button = document.createElement("button");
      button.type = "button";
      button.className = "task-history-item" + (task.id === state.selectedTaskId ? " is-selected" : "");
      button.innerHTML =
        '<strong class="badge-pill ' + task.status + '">' + task.status + '</strong>' +
        '<span>' + escapeHtml(task.prompt) + '</span>' +
        '<small>' + formatTime(task.createdAt) + '</small>';
      button.addEventListener("click", function() { selectTask(task.id); });
      nodes.taskList.appendChild(button);
    });
  }

  function selectTask(taskID) {
    state.selectedTaskId = taskID;
    renderTaskList(state.tasks);
    refreshTask();
    connectEvents();
  }

  function refreshTask() {
    if (!state.selectedTaskId) return;
    api("/api/tasks/" + state.selectedTaskId).then(renderTask).catch(showError);
  }

  function runTask() {
    if (!state.selectedTaskId) { showError(new Error("No task selected.")); return; }
    api("/api/tasks/" + state.selectedTaskId + "/run", { method: "POST" })
      .then(function (result) { notice("Task run: " + result.status); refreshTask(); loadTasks(); })
      .catch(showError);
  }

  function cancelTask() {
    if (!state.selectedTaskId) { showError(new Error("No task selected.")); return; }
    api("/api/tasks/" + state.selectedTaskId + "/cancel", { method: "POST" })
      .then(function () { notice("Cancel requested."); refreshTask(); loadTasks(); })
      .catch(showError);
  }

  // ── Approvals ────────────────────────────────────────────────────────────

  function loadApprovals() {
    api("/api/approvals?status=pending").then(function (approvals) {
      var count = (approvals || []).length;
      // Update the view count badge
      if (nodes.approvalCount) nodes.approvalCount.textContent = String(count);
      // Update the nav sidebar badge
      if (nodes.navApprovalBadge) {
        nodes.navApprovalBadge.textContent = count > 0 ? String(count) : "";
        nodes.navApprovalBadge.style.display = count > 0 ? "flex" : "none";
      }
      nodes.approvalList.innerHTML = "";
      (approvals || []).forEach(renderApproval);
    }).catch(showError);
  }

  function renderApproval(approval) {
    var item = document.createElement("div");
    item.className = "approval-card-styled";
    var planHtml = "";
    if (approval.actionType === "command_execution" && approval.payloadJson) {
      try {
        var parsed = JSON.parse(approval.payloadJson);
        if (parsed.CommandLine) {
          planHtml = '<div class="plan-container"><code>$ ' + escapeHtml(parsed.CommandLine) + '</code></div>';
        }
      } catch(e) {}
    }
    item.innerHTML =
      '<div style="display:flex;justify-content:space-between;align-items:center;">' +
        '<strong style="font-size:12px;font-weight:600;text-transform:uppercase;">' + escapeHtml(approval.actionType) + '</strong>' +
        '<span class="badge-pill waiting_for_approval">Pending</span>' +
      '</div>' +
      '<p style="font-size:13px;color:var(--muted);margin-top:4px;">' + escapeHtml(approval.description) + '</p>' +
      planHtml +
      '<div class="button-row" style="margin-top:8px;"></div>';

    var buttons = item.querySelector(".button-row");
    buttons.appendChild(actionButton("Approve", function () { resolveApproval(approval.id, "approve"); }));
    buttons.appendChild(actionButton("Reject", function () { resolveApproval(approval.id, "reject"); }));
    if (approval.actionType === "command_execution") {
      buttons.appendChild(actionButton("Execute", function () { executeApproval(approval.id); }));
    }
    nodes.approvalList.appendChild(item);
  }

  function resolveApproval(id, action) {
    api("/api/approvals/" + id + "/" + action, { method: "POST" })
      .then(function () { notice("Approval " + action + "d."); loadApprovals(); })
      .catch(showError);
  }

  function executeApproval(id) {
    api("/api/approvals/" + id + "/execute", { method: "POST" })
      .then(function (result) { notice("Executed: " + result.status); loadApprovals(); })
      .catch(showError);
  }

  // ── Event stream ─────────────────────────────────────────────────────────

  function connectEvents() {
    if (!state.selectedTaskId) return;
    if (state.eventSource) { state.eventSource.close(); }
    nodes.eventLog.innerHTML = "";
    var url = "/api/tasks/" + state.selectedTaskId + "/events/stream";
    state.eventSource = new EventSource(url);
    state.eventSource.onmessage = addEventLine;
    ["task.created", "agent.started", "agent.completed", "agent.failed", "agent.canceled",
     "command.blocked", "command.started", "command.completed", "command.failed",
     "approval.requested", "approval.approved", "approval.rejected", "approval.executed"
    ].forEach(function(name) { state.eventSource.addEventListener(name, addEventLine); });
    state.eventSource.onerror = function () { notice("Event stream disconnected."); };
  }

  function addEventLine(event) {
    if (!event.data) return;
    var payload;
    try { payload = JSON.parse(event.data); } catch(e) { return; }

    // Auto-refresh on completion events
    if (payload.eventType === "agent.completed" || payload.eventType === "agent.failed" || payload.eventType === "agent.canceled") {
      loadTasks();
    }

    var item = document.createElement("li");
    item.className = "timeline-item";
    item.innerHTML =
      '<div class="timeline-marker active"></div>' +
      '<div class="timeline-content">' +
        '<span class="timeline-time">' + new Date(payload.createdAt).toLocaleTimeString() + '</span>' +
        '<strong class="timeline-title">' + escapeHtml(payload.eventType) + '</strong>' +
        '<p class="timeline-body">' + escapeHtml(payload.message) + '</p>' +
      '</div>';
    nodes.eventLog.prepend(item);
  }

  // ── Select project ───────────────────────────────────────────────────────

  function selectProject() {
    state.selectedProjectId = nodes.projectSelect.value;
    state.selectedTaskId = "";
    nodes.taskStatus.textContent = "None";
    nodes.taskDetail.innerHTML = "";

    var project = state.projects.find(function(p) { return p.id === state.selectedProjectId; });
    if (project) {
      nodes.projectDetailTech.textContent = project.techStack || "None";
      nodes.projectDetailTestCmd.textContent = project.testCommand || "None";
      nodes.headerProjectName.textContent = project.name;
    } else {
      nodes.headerProjectName.textContent = "No project selected";
      nodes.projectDetailTech.textContent = "None";
      nodes.projectDetailTestCmd.textContent = "None";
    }

    nodes.projectGitCommitBox.style.display = "none";
    nodes.projectGitCommitMsg.value = "";
    nodes.projectTestResultBox.style.display = "none";
    nodes.projectTestResultBox.textContent = "";

    // Refresh sidebar active states
    document.querySelectorAll(".project-card-mini").forEach(function(card) {
      card.classList.toggle("active", card.querySelector(".title") && card.querySelector(".title").textContent === (project ? project.name : ""));
    });

    if (state.isEditingProject) cancelEditProject();
    loadTasks();
  }

  // ── Edit / Delete project ─────────────────────────────────────────────────

  function editProject() {
    var project = state.projects.find(function(p) { return p.id === state.selectedProjectId; });
    if (!project) { showError(new Error("No project selected.")); return; }
    state.isEditingProject = true;
    nodes.projectForm.querySelector("input[name='name']").value = project.name;
    nodes.projectForm.querySelector("input[name='path']").value = project.path;
    nodes.projectForm.querySelector("input[name='techStack']").value = project.techStack || "";
    nodes.projectForm.querySelector("input[name='testCommand']").value = project.testCommand || "";
    nodes.projectSubmitBtn.textContent = "Save Changes";
    $("modal-project-title").textContent = "Edit Project";
    $("project-modal").style.display = "flex";
  }

  function cancelEditProject() {
    state.isEditingProject = false;
    nodes.projectForm.reset();
    nodes.projectSubmitBtn.textContent = "Add Project";
    closeProjectModal();
  }

  function deleteProject() {
    if (!state.selectedProjectId) { showError(new Error("No project selected.")); return; }
    if (!confirm("Delete this project and all its task history?")) return;
    api("/api/projects/" + encodeURIComponent(state.selectedProjectId), { method: "DELETE" })
      .then(function () {
        state.selectedProjectId = "";
        state.selectedTaskId = "";
        cancelEditProject();
        notice("Project deleted.");
        loadProjects();
      }).catch(showError);
  }

  // ── Git controls ─────────────────────────────────────────────────────────

  function generateCommitMsg() {
    if (!state.selectedProjectId) { showError(new Error("No project selected.")); return; }
    api("/api/projects/" + encodeURIComponent(state.selectedProjectId) + "/git/commit-message", { method: "POST" })
      .then(function (res) {
        if (res.message === "No changes to commit") {
          notice("No changes to commit."); nodes.projectGitCommitBox.style.display = "none"; return;
        }
        nodes.projectGitCommitMsg.value = res.message;
        nodes.projectGitCommitBox.style.display = "block";
      }).catch(showError);
  }

  function gitCommit() {
    if (!state.selectedProjectId) return;
    var msg = nodes.projectGitCommitMsg.value.trim();
    if (!msg) { showError(new Error("Commit message is empty.")); return; }
    api("/api/projects/" + encodeURIComponent(state.selectedProjectId) + "/git/commit", {
      method: "POST", body: JSON.stringify({ message: msg })
    }).then(function (res) {
      if (res.status === "failed") { showError(new Error("Commit failed: " + (res.stderr || "Unknown"))); return; }
      notice("Committed.");
      nodes.projectGitCommitBox.style.display = "none";
      nodes.projectGitCommitMsg.value = "";
    }).catch(showError);
  }

  function gitPush() {
    if (!state.selectedProjectId) { showError(new Error("No project selected.")); return; }
    notice("Pushing…");
    api("/api/projects/" + encodeURIComponent(state.selectedProjectId) + "/git/push", { method: "POST" })
      .then(function (res) {
        if (res.status === "failed") { showError(new Error("Push failed: " + (res.stderr || "Unknown"))); return; }
        notice("Push complete.");
      }).catch(showError);
  }

  // ── Run tests ────────────────────────────────────────────────────────────

  function runProjectTests() {
    if (!state.selectedProjectId) { showError(new Error("No project selected.")); return; }
    notice("Running tests…");
    nodes.projectTestResultBox.style.display = "block";
    nodes.projectTestResultBox.style.borderColor = "var(--border)";
    nodes.projectTestResultBox.textContent = "⚙️ Running…";
    api("/api/projects/" + encodeURIComponent(state.selectedProjectId) + "/tests/run", { method: "POST" })
      .then(function (res) {
        var output = (res.stdout || "") + (res.stderr ? "\n" + res.stderr : "") || "No output.";
        nodes.projectTestResultBox.textContent = output;
        if (res.exitCode === 0) {
          notice("Tests passed ✅");
          nodes.projectTestResultBox.style.borderColor = "var(--success)";
        } else {
          showError(new Error("Tests failed (exit " + res.exitCode + ")"));
          nodes.projectTestResultBox.style.borderColor = "var(--error)";
        }
      }).catch(function (err) {
        nodes.projectTestResultBox.textContent = "❌ " + err.message;
        nodes.projectTestResultBox.style.borderColor = "var(--error)";
        showError(err);
      });
  }

  // ── Rendering helpers ────────────────────────────────────────────────────

  function escapeHtml(str) {
    if (!str) return "";
    return str
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }

  function formatTime(isoString) {
    if (!isoString) return "";
    return new Date(isoString).toLocaleString();
  }

  function formatMarkdown(text) {
    if (!text) return "";
    var escaped = escapeHtml(text);
    var parts = escaped.split(/```(\w*)\n([\s\S]*?)```/g);
    var html = "";
    for (var i = 0; i < parts.length; i++) {
      if (i % 3 === 0) {
        html += parts[i].replace(/\n/g, "<br>");
      } else if (i % 3 === 1) {
        var lang = parts[i] || "code";
        var codeContent = parts[i + 1] || "";
        html +=
          '<div class="code-window-card">' +
            '<div class="code-window-header">' +
              '<span class="language">' + lang + '</span>' +
              '<button type="button" class="btn-copy-code">Copy</button>' +
            '</div>' +
            '<pre><code>' + codeContent + '</code></pre>' +
          '</div>';
        i++;
      }
    }
    return html;
  }

  function renderConversation(tasks) {
    var history = $("chat-history");
    if (!history) return;

    if (!tasks || !tasks.length) {
      history.innerHTML =
        '<div class="chat-empty-state">' +
          '<div class="empty-icon">✻</div>' +
          '<h2>How can I help you today?</h2>' +
          '<p>Select a project and start a conversation, or try one of these prompts.</p>' +
          '<div class="suggestion-grid">' +
            '<button type="button" class="btn-prompt-suggestion" data-prompt="Analyze this project and summarize its architecture">' +
              '<span class="sg-icon">🔍</span>' +
              '<span>Analyze project</span>' +
            '</button>' +
            '<button type="button" class="btn-prompt-suggestion" data-prompt="Check connection and environment config">' +
              '<span class="sg-icon">⚡</span>' +
              '<span>Verify environment</span>' +
            '</button>' +
            '<button type="button" class="btn-prompt-suggestion" data-prompt="Write a Hello World test script">' +
              '<span class="sg-icon">📝</span>' +
              '<span>Write hello world</span>' +
            '</button>' +
            '<button type="button" class="btn-prompt-suggestion" data-prompt="Run tests and report results">' +
              '<span class="sg-icon">✅</span>' +
              '<span>Run tests</span>' +
            '</button>' +
          '</div>' +
        '</div>';
      return;
    }

    history.innerHTML = "";
    // Reverse tasks so oldest is rendered first (top) and newest is rendered last (bottom)
    var sortedTasks = tasks.slice().reverse();
    sortedTasks.forEach(function (task) {
      // User bubble
      var userMsg = document.createElement("div");
      userMsg.className = "chat-message user" + (task.id === state.selectedTaskId ? " active" : "");
      userMsg.innerHTML =
        '<div class="chat-avatar user-avatar">U</div>' +
        '<div class="message-content-wrapper">' +
          '<div class="message-meta">You &bull; ' + formatTime(task.createdAt) + '</div>' +
          '<div class="message-bubble">' + escapeHtml(task.prompt) + '</div>' +
        '</div>';
      history.appendChild(userMsg);

      // Assistant bubble
      var contentHtml = "";
      if (task.status === "queued") {
        contentHtml = '<p class="thinking-indicator">Queued…</p>';
      } else if (task.status === "running") {
        contentHtml = '<p class="thinking-indicator">Agent is running…</p>';
      } else {
        if (task.stdout) contentHtml += '<div class="formatted-output">' + formatMarkdown(task.stdout) + '</div>';
        if (task.stderr) {
          contentHtml +=
            '<div class="console-section error" style="margin-top:8px;">' +
              '<strong>Stderr:</strong>' +
              '<pre style="padding:10px;border-radius:var(--rounded-md);background:#1e293b;color:#f87171;font-size:12px;white-space:pre-wrap;margin-top:4px;"><code>' + escapeHtml(task.stderr) + '</code></pre>' +
            '</div>';
        }
        if (task.errorMessage) {
          contentHtml +=
            '<div class="console-section error" style="margin-top:8px;">' +
              '<strong>Error:</strong>' +
              '<pre style="padding:10px;border-radius:var(--rounded-md);background:#1e293b;color:#f87171;font-size:12px;white-space:pre-wrap;margin-top:4px;"><code>' + escapeHtml(task.errorMessage) + '</code></pre>' +
            '</div>';
        }
        if (!task.stdout && !task.stderr && !task.errorMessage) {
          contentHtml = '<p style="color:var(--muted);font-style:italic;">No output yet.</p>';
        }
      }

      var assistantMsg = document.createElement("div");
      assistantMsg.className = "chat-message assistant";
      assistantMsg.innerHTML =
        '<div class="chat-avatar">✻</div>' +
        '<div class="message-content-wrapper">' +
          '<div class="message-meta">Agent &bull; ' + formatTime(task.completedAt || task.startedAt || task.createdAt) + ' &bull; <span class="badge-pill ' + task.status + '">' + task.status + '</span></div>' +
          '<div class="message-bubble" style="background:var(--bg-subtle);">' + contentHtml + '</div>' +
        '</div>';
      history.appendChild(assistantMsg);
    });

    history.scrollTop = history.scrollHeight;
  }

  function renderTask(task) {
    state.selectedTaskId = task ? task.id : "";
    if (!task) {
      nodes.taskStatus.textContent = "None";
      nodes.taskDetail.innerHTML = "";
      return;
    }
    nodes.taskStatus.textContent = task.status;

    var stdoutHtml = task.stdout ? '<div class="console-section"><strong>Output:</strong><pre>' + escapeHtml(task.stdout) + '</pre></div>' : "";
    var stderrHtml = task.stderr ? '<div class="console-section error"><strong>Errors:</strong><pre>' + escapeHtml(task.stderr) + '</pre></div>' : "";
    var errorHtml = task.errorMessage ? '<div class="console-section error"><strong>System Error:</strong><pre>' + escapeHtml(task.errorMessage) + '</pre></div>' : "";

    nodes.taskDetail.innerHTML =
      '<div class="task-info-card">' +
        '<div class="task-info-header">' +
          '<span class="badge-pill ' + task.status + '">' + task.status + '</span>' +
          '<span style="font-size:11px;color:var(--muted);">' + escapeHtml(task.agentType) + '</span>' +
          '<span style="font-size:11px;color:var(--muted);">' + formatTime(task.createdAt) + '</span>' +
        '</div>' +
        '<div class="task-prompt-text"><strong>Prompt:</strong> ' + escapeHtml(task.prompt) + '</div>' +
        stdoutHtml + stderrHtml + errorHtml +
      '</div>';
  }

  function actionButton(label, handler) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = label;
    btn.addEventListener("click", handler);
    return btn;
  }

  function notice(msg) {
    nodes.notice.textContent = msg;
    nodes.notice.style.display = "block";
    clearTimeout(nodes.notice._timer);
    nodes.notice._timer = setTimeout(function() { nodes.notice.style.display = "none"; }, 4000);
  }

  function showError(err) {
    nodes.notice.textContent = "⚠ " + err.message;
    nodes.notice.style.display = "block";
    clearTimeout(nodes.notice._timer);
    nodes.notice._timer = setTimeout(function() { nodes.notice.style.display = "none"; }, 5000);
  }

  document.addEventListener("DOMContentLoaded", init);
}());
