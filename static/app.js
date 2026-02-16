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
    // Filter table rows and hide tables when nothing selected
    if (selectedUsers.length === 0) {
        document.querySelectorAll('table').forEach(function(t) { t.style.display = 'none'; });
        document.querySelectorAll('h2[data-i18n="recent_redemptions"], h2[data-i18n="recent_stars"]').forEach(function(h) { h.style.display = 'none'; });
    } else {
        document.querySelectorAll('table').forEach(function(t) { t.style.display = ''; });
        document.querySelectorAll('h2[data-i18n="recent_redemptions"], h2[data-i18n="recent_stars"]').forEach(function(h) { h.style.display = ''; });
        document.querySelectorAll('table tbody tr[data-username]').forEach(function(row) {
            row.style.display = (selectedUsers.indexOf(row.dataset.username) !== -1) ? '' : 'none';
        });
    }
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

function submitStarByReason(reasonId) {
    var item = document.querySelector('.reason-item[data-reason-id="' + reasonId + '"]');
    var langKey = currentLang === 'zh-CN' ? 'zh-cn' : (currentLang === 'zh-TW' ? 'zh-tw' : 'en');
    var reasonText = item.getAttribute('data-' + langKey) || item.getAttribute('data-en');
    submitStar(reasonText, reasonId);
}

function submitStar(reason, reasonId, stars) {
    var targets = getSelectedNonSelf();
    if (!reason || targets.length === 0) return;

    function awardNext(i) {
        if (i >= targets.length) return;
        var username = targets[i];
        var body = new URLSearchParams({username: username});
        if (reasonId) {
            body.append('reason_id', reasonId);
        }
        body.append('reason', reason);
        if (stars && stars > 0) {
            body.append('stars', stars);
        }

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
            tr.dataset.starId = data.starId || '';
            var now = new Date();
            var month = now.toLocaleString('en-US', {month: 'short'});
            var time = month + ' ' + now.getDate() + ' ' + now.getHours().toString().padStart(2,'0') + ':' + now.getMinutes().toString().padStart(2,'0');

            // Get translations from the reason item if it's a predefined reason
            var reasonTd = '<td class="star-reason"';
            if (reasonId) {
                var item = document.querySelector('.reason-item[data-reason-id="' + reasonId + '"]');
                if (item) {
                    reasonTd += ' data-en="' + (item.getAttribute('data-en') || '') + '"';
                    reasonTd += ' data-zh-cn="' + (item.getAttribute('data-zh-cn') || '') + '"';
                    reasonTd += ' data-zh-tw="' + (item.getAttribute('data-zh-tw') || '') + '"';
                }
            }
            reasonTd += '>' + reason + '</td>';

            // Check if user is admin to add undo button
            var actionBar = document.getElementById('actionBar');
            var actionTd = actionBar ? '<td><button class="btn-undo" onclick="undoStar(' + (data.starId || '') + ')" title="Remove this star">✕</button></td>' : '<td></td>';

            tr.innerHTML = '<td>' + username + '</td>' + reasonTd + '<td>' + data.awardedBy + '</td><td>' + time + '</td>' + actionTd;
            tbody.insertBefore(tr, tbody.firstChild);
            playStarAnim(username, '⭐');
            awardNext(i + 1);
        });
    }
    awardNext(0);
    var ci = document.getElementById('customReason');
    if (ci) ci.value = '';
    var si = document.getElementById('customStars');
    if (si) si.value = '1';
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

function editUserTrans(userId, lang, cell) {
    var currentText = cell.textContent;
    var input = document.createElement('input');
    input.type = 'text';
    input.value = currentText;
    input.style.width = '100%';

    function save() {
        var newText = input.value.trim();
        if (newText && newText !== currentText) {
            var body = new URLSearchParams({lang: lang, text: newText});
            fetch("/admin/user/" + userId, {
                method: "PUT",
                body: body
            })
            .then(function(resp) { return resp.json(); })
            .then(function() {
                cell.textContent = newText;
            });
        } else {
            cell.textContent = currentText;
        }
    }

    input.onblur = save;
    input.onkeydown = function(e) {
        if (e.key === 'Enter') {
            e.preventDefault();
            save();
        } else if (e.key === 'Escape') {
            cell.textContent = currentText;
        }
    };

    cell.textContent = '';
    cell.appendChild(input);
    input.focus();
    input.select();
}

function editReasonTrans(reasonId, lang, cell) {
    var currentText = cell.textContent;
    var input = document.createElement('input');
    input.type = 'text';
    input.value = currentText;
    input.style.width = '100%';

    function save() {
        var newText = input.value.trim();
        if (newText && newText !== currentText) {
            var body = new URLSearchParams({lang: lang, text: newText});
            fetch("/admin/reason/" + reasonId, {
                method: "PUT",
                body: body
            })
            .then(function(resp) { return resp.json(); })
            .then(function() {
                cell.textContent = newText;
            });
        } else {
            cell.textContent = currentText;
        }
    }

    input.onblur = save;
    input.onkeydown = function(e) {
        if (e.key === 'Enter') {
            e.preventDefault();
            save();
        } else if (e.key === 'Escape') {
            cell.textContent = currentText;
        }
    };

    cell.textContent = '';
    cell.appendChild(input);
    input.focus();
    input.select();
}

function editReasonStars(reasonId, cell) {
    var currentValue = cell.textContent;
    var input = document.createElement('input');
    input.type = 'number';
    input.min = '1';
    input.value = currentValue;
    input.style.width = '4rem';
    input.style.textAlign = 'center';

    function save() {
        var newValue = parseInt(input.value, 10);
        if (newValue >= 1 && newValue.toString() !== currentValue) {
            var body = new URLSearchParams({stars: newValue});
            fetch("/admin/reason/" + reasonId, {
                method: "PUT",
                body: body
            })
            .then(function(resp) { return resp.json(); })
            .then(function() {
                cell.textContent = newValue;
            });
        } else {
            cell.textContent = currentValue;
        }
    }

    input.onblur = save;
    input.onkeydown = function(e) {
        if (e.key === 'Enter') {
            e.preventDefault();
            save();
        } else if (e.key === 'Escape') {
            cell.textContent = currentValue;
        }
    };

    cell.textContent = '';
    cell.appendChild(input);
    input.focus();
    input.select();
}

function deleteReasonEntry(id) {
    if (!confirm("Delete this reason and all its translations?")) return;
    fetch("/admin/reason/" + id, { method: "DELETE" })
        .then(function() { location.reload(); });
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

function editRewardTrans(rewardId, lang, cell) {
    var currentText = cell.textContent;
    var input = document.createElement('input');
    input.type = 'text';
    input.value = currentText;
    input.style.width = '100%';

    function save() {
        var newText = input.value.trim();
        if (newText && newText !== currentText) {
            var body = new URLSearchParams({lang: lang, text: newText});
            fetch("/admin/reward/" + rewardId, {
                method: "PUT",
                body: body
            })
            .then(function(resp) { return resp.json(); })
            .then(function() {
                cell.textContent = newText;
            });
        } else {
            cell.textContent = currentText;
        }
    }

    input.onblur = save;
    input.onkeydown = function(e) {
        if (e.key === 'Enter') {
            e.preventDefault();
            save();
        } else if (e.key === 'Escape') {
            cell.textContent = currentText;
        }
    };

    cell.textContent = '';
    cell.appendChild(input);
    input.focus();
    input.select();
}

function editRewardCost(rewardId, cell) {
    var currentValue = cell.textContent;
    var input = document.createElement('input');
    input.type = 'number';
    input.min = '1';
    input.value = currentValue;
    input.style.width = '4rem';
    input.style.textAlign = 'center';

    function save() {
        var newValue = parseInt(input.value, 10);
        if (newValue >= 1 && newValue.toString() !== currentValue) {
            var body = new URLSearchParams({cost: newValue});
            fetch("/admin/reward/" + rewardId, {
                method: "PUT",
                body: body
            })
            .then(function(resp) { return resp.json(); })
            .then(function() {
                cell.textContent = newValue;
            });
        } else {
            cell.textContent = currentValue;
        }
    }

    input.onblur = save;
    input.onkeydown = function(e) {
        if (e.key === 'Enter') {
            e.preventDefault();
            save();
        } else if (e.key === 'Escape') {
            cell.textContent = currentValue;
        }
    };

    cell.textContent = '';
    cell.appendChild(input);
    input.focus();
    input.select();
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

// Auto-select self for non-admin users on page load
document.addEventListener('DOMContentLoaded', function() {
    var actionBar = document.getElementById('actionBar');
    // If no action bar, user is not admin - select self by default
    if (!actionBar) {
        var selfCard = document.querySelector('.member-card[data-self="true"]');
        if (selfCard) {
            selectedUsers = [selfCard.dataset.username];
            updateSelection();
        }
    }
});
