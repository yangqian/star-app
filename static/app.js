var selectedUsers = [];
var selectionMode = 'individual'; // 'individual' or 'multiple'

function setSelectionMode(mode) {
    selectionMode = mode;
    document.querySelectorAll('.mode-btn').forEach(function(b) {
        b.classList.toggle('active', b.dataset.mode === mode);
    });
    // When switching to individual and multiple are selected, keep only the first
    if (mode === 'individual' && selectedUsers.length > 1) {
        selectedUsers = [selectedUsers[0]];
        updateSelection();
    }
}

function getSelectedNonSelf() {
    return selectedUsers.filter(function(u) {
        var card = document.querySelector('.member-card[data-username="' + u + '"]');
        return card && card.dataset.self !== 'true';
    });
}

function updateSelection() {
    document.querySelectorAll('.member-card').forEach(function(c) {
        c.classList.toggle('selected', selectedUsers.indexOf(c.dataset.username) !== -1);
    });
    // Filter table rows: show only selected users' rows (or all if none selected)
    var show = selectedUsers.length === 0 ? null : selectedUsers;
    document.querySelectorAll('table tbody tr[data-username]').forEach(function(row) {
        row.style.display = (!show || show.indexOf(row.dataset.username) !== -1) ? '' : 'none';
    });
    // Update action bar
    var actionBar = document.getElementById('actionBar');
    if (!actionBar) return;
    var awardable = getSelectedNonSelf();
    if (selectedUsers.length > 0) {
        actionBar.style.display = 'flex';
        document.getElementById('selectedName').textContent = selectedUsers.join(', ');
        var buttons = actionBar.querySelectorAll('button');
        buttons.forEach(function(b) { b.style.display = awardable.length > 0 ? '' : 'none'; });
    } else {
        actionBar.style.display = 'none';
    }
    document.getElementById('reasonPanel').style.display = 'none';
    document.getElementById('redeemPanel').style.display = 'none';
    // Sync filter buttons
    syncFilterButtons();
}

function syncFilterButtons() {
    var kids = [], parents = [], all = [];
    document.querySelectorAll('.member-card').forEach(function(c) {
        all.push(c.dataset.username);
        if (c.dataset.role === 'kid') kids.push(c.dataset.username);
        else parents.push(c.dataset.username);
    });
    var sorted = selectedUsers.slice().sort();
    document.querySelectorAll('.filter-btn').forEach(function(b) {
        var label = b.textContent.toLowerCase();
        var active = false;
        if (label === 'all') active = arrEq(sorted, all.slice().sort());
        else if (label === 'kids') active = arrEq(sorted, kids.slice().sort());
        else if (label === 'parents') active = arrEq(sorted, parents.slice().sort());
        else active = selectedUsers.length === 1 && selectedUsers[0].toLowerCase() === label;
        b.classList.toggle('active', active);
    });
}

function arrEq(a, b) {
    if (a.length !== b.length) return false;
    for (var i = 0; i < a.length; i++) { if (a[i] !== b[i]) return false; }
    return true;
}

function selectUser(username) {
    if (selectionMode === 'individual') {
        // Click switches to this user (or deselects if already the only one)
        if (selectedUsers.length === 1 && selectedUsers[0] === username) {
            selectedUsers = [];
        } else {
            selectedUsers = [username];
        }
    } else {
        // Multiple: toggle add/remove
        var idx = selectedUsers.indexOf(username);
        if (idx !== -1) selectedUsers.splice(idx, 1);
        else selectedUsers.push(username);
    }
    updateSelection();
}

function filterCards(filter) {
    // Group filters switch to multiple mode; name filters stay individual
    var isGroup = (filter === 'all' || filter === 'kids' || filter === 'parents');
    if (isGroup) setSelectionMode('multiple');
    selectedUsers = [];
    document.querySelectorAll('.member-card').forEach(function(c) {
        if (filter === 'all') selectedUsers.push(c.dataset.username);
        else if (filter === 'kids' && c.dataset.role === 'kid') selectedUsers.push(c.dataset.username);
        else if (filter === 'parents' && c.dataset.role === 'parent') selectedUsers.push(c.dataset.username);
        else if (filter === c.dataset.username) selectedUsers.push(c.dataset.username);
    });
    updateSelection();
}

function togglePanel(id) {
    var panels = ['reasonPanel', 'redeemPanel'];
    panels.forEach(function(pid) {
        var p = document.getElementById(pid);
        if (pid === id) {
            p.style.display = p.style.display === 'none' ? 'block' : 'none';
        } else {
            p.style.display = 'none';
        }
    });
}

function updateStarCounts(counts) {
    counts.forEach(function(c) {
        var card = document.querySelector('.member-card[data-username="' + c.Username + '"]');
        if (!card) return;
        card.querySelector('.star-number').textContent = c.CurrentStars;
        card.querySelector('.star-total').textContent = c.StarCount + ' total earned';
    });
}

function playStarAnim(username, emoji) {
    var card = document.querySelector('.member-card[data-username="' + username + '"]');
    if (card) {
        var el = document.createElement('span');
        el.className = 'star-anim';
        el.textContent = emoji || '⭐';
        card.appendChild(el);
        el.addEventListener('animationend', function() { el.remove(); });
    }
}

function submitStar(reason) {
    var targets = getSelectedNonSelf();
    if (!reason || targets.length === 0) return;

    function awardNext(i) {
        if (i >= targets.length) return;
        var username = targets[i];
        var body = new URLSearchParams({username: username, reason: reason});
        fetch("/star", {
            method: "POST",
            headers: {"Accept": "application/json"},
            body: body
        })
        .then(function(resp) {
            if (!resp.ok) return resp.text().then(function(t) { alert(t); return null; });
            return resp.json();
        })
        .then(function(data) {
            if (!data) return;
            updateStarCounts(data.counts);
            var tbody = document.querySelectorAll('table')[1].querySelector('tbody');
            var noRows = tbody.querySelector('td[colspan]');
            if (noRows) tbody.innerHTML = '';
            var tr = document.createElement('tr');
            tr.dataset.username = username;
            var now = new Date();
            var month = now.toLocaleString('en-US', {month: 'short'});
            var time = month + ' ' + now.getDate() + ' ' + now.getHours().toString().padStart(2,'0') + ':' + now.getMinutes().toString().padStart(2,'0');
            tr.innerHTML = '<td>' + username + '</td><td>' + reason + '</td><td>' + data.awardedBy + '</td><td>' + time + '</td>';
            tbody.insertBefore(tr, tbody.firstChild);
            playStarAnim(username, '⭐');
            awardNext(i + 1);
        });
    }
    awardNext(0);
    var ci = document.getElementById('customReason');
    if (ci) ci.value = '';
}

function submitRedeem(rewardId, rewardName, cost) {
    var targets = getSelectedNonSelf();
    if (targets.length === 0) return;
    var names = targets.join(', ');
    if (!confirm("Spend " + cost + " stars each for " + names + " on \"" + rewardName + "\"?")) return;

    function redeemNext(i) {
        if (i >= targets.length) return;
        var username = targets[i];
        var body = new URLSearchParams({reward_id: rewardId, username: username});
        fetch("/redeem", {
            method: "POST",
            headers: {"Accept": "application/json"},
            body: body
        })
        .then(function(resp) {
            if (!resp.ok) return resp.text().then(function(t) { alert(t); return null; });
            return resp.json();
        })
        .then(function(data) {
            if (!data) return;
            updateStarCounts(data.counts);
            var tbody = document.querySelectorAll('table')[0].querySelector('tbody');
            var noRows = tbody.querySelector('td[colspan]');
            if (noRows) tbody.innerHTML = '';
            var tr = document.createElement('tr');
            tr.dataset.username = username;
            var now = new Date();
            var month = now.toLocaleString('en-US', {month: 'short'});
            var time = month + ' ' + now.getDate() + ' ' + now.getHours().toString().padStart(2,'0') + ':' + now.getMinutes().toString().padStart(2,'0');
            tr.innerHTML = '<td>' + username + '</td><td>' + data.rewardName + '</td><td>' + data.cost + ' ⭐</td><td>' + time + '</td>';
            tbody.insertBefore(tr, tbody.firstChild);
            playStarAnim(username, '🎁');
            redeemNext(i + 1);
        });
    }
    redeemNext(0);
}

function undoStar(id) {
    if (!confirm("Remove this star?")) return;
    fetch("/star/" + id, { method: "DELETE" })
    .then(function(resp) { return resp.json(); })
    .then(function(counts) {
        var row = document.querySelector('tr[data-star-id="' + id + '"]');
        if (row) row.remove();
        updateStarCounts(counts);
    });
}

function deleteReward(id) {
    if (!confirm("Delete this reward?")) return;
    fetch("/admin/reward/" + id, { method: "DELETE" })
        .then(function() { location.reload(); });
}

function deleteKey(id) {
    if (!confirm("Revoke this API key?")) return;
    fetch("/admin/apikey/" + id, { method: "DELETE" })
        .then(function() { location.reload(); });
}

function toggleAnnounce() {
    fetch("/admin/toggle-announce", { method: "POST" })
    .then(function(resp) { return resp.json(); })
    .then(function(data) {
        var btn = document.getElementById('announceToggle');
        var on = data.ha_enabled === '1';
        btn.classList.toggle('on', on);
        var span = btn.querySelector('span');
        var dict = translations[currentLang] || translations.en;
        span.textContent = on ? (dict.announce_on || 'On') : (dict.announce_off || 'Off');
        span.setAttribute('data-i18n', on ? 'announce_on' : 'announce_off');
    });
}
