package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"math/rand"
	"raft/labrpc"
	"sync"
	"sync/atomic"
	"time"
)

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu    sync.Mutex          // Lock to protect shared access to this peer's state
	peers []*labrpc.ClientEnd // RPC end points of all peers
	me    int                 // this peer's index into peers[]
	dead  int32               // set by Kill()

	// Your data here (2A, 2B).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	// You may also need to add other state, as per your implementation.

	// 2A
	currentTerm   int
	votedFor      int
	role          Role
	lastHeartBeat time.Time

	// Volatile state on all servers
	log         []LogEntry
	commitIndex int
	lastApplied int

	// Volatile state on leaders
	// (Reinitialized after election)

	// nextIndex keeps tracks of the where the follower is consistent
	nextIndex  []int
	matchIndex []int
}

type LogEntry struct {
	Command interface{}
	Term    int
}

type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm
	isleader = rf.role == Leader
	return term, isleader
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         int
	LeaderID     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

func (rf *Raft) becomeLeaderLocked() {
	rf.role = Leader
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))
	for peer := range rf.peers {
		rf.nextIndex[peer] = len(rf.log)
		rf.matchIndex[peer] = 0
	}
	rf.matchIndex[rf.me] = len(rf.log) - 1
}

func (rf *Raft) startElection() {
	rf.mu.Lock()
	if rf.role != Candidate {
		rf.mu.Unlock()
		return
	}
	rf.currentTerm += 1
	lastLogIndex := len(rf.log) - 1
	lastLogTerm := rf.log[lastLogIndex].Term //last
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	currentTerm := rf.currentTerm
	me := rf.me
	rf.votedFor = rf.me
	rf.lastHeartBeat = time.Now()
	majority := len(rf.peers)/2 + 1
	if majority == 1 {
		rf.becomeLeaderLocked()
		rf.mu.Unlock()
		return
	}
	rf.mu.Unlock()

	count := 1 // vote for self

	for i := 0; i < len(rf.peers); i++ {
		if i == me {
			continue
		}
		go func(peerID int) {
			var reply RequestVoteReply
			ok := rf.sendRequestVote(peerID, &args, &reply)
			if !ok {
				return
			}

			rf.mu.Lock()
			defer rf.mu.Unlock()

			// step down if we see a higher term
			if reply.Term > currentTerm {
				rf.currentTerm = reply.Term
				rf.role = Follower
				rf.votedFor = -1
				return
			}

			// stale reply
			if rf.currentTerm != currentTerm || rf.role != Candidate {
				return
			}

			if reply.VoteGranted {
				count++
			}

			if count >= majority && rf.role != Leader {
				rf.becomeLeaderLocked()
			}
		}(i)
	}
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	// Read the fields in "args",
	// and accordingly assign the values for fields in "reply".

	rf.mu.Lock()
	defer rf.mu.Unlock()

	// reject
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		return
	}

	// step down
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.role = Follower
		rf.votedFor = -1
	}

	// grant vote if votedFor is null or candidateId, and candidate's log is at least as up-to-date (§5.4)
	myLastIndex := len(rf.log) - 1
	myLastTerm := rf.log[myLastIndex].Term
	candidateUpToDate := args.LastLogTerm > myLastTerm ||
		(args.LastLogTerm == myLastTerm && args.LastLogIndex >= myLastIndex)

	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) && candidateUpToDate {
		reply.Term = rf.currentTerm
		reply.VoteGranted = true
		rf.votedFor = args.CandidateId
		rf.lastHeartBeat = time.Now()
		return
	}

	reply.Term = rf.currentTerm
	reply.VoteGranted = false

}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// AppendEntries RPC Handler
// TODO: Add impl for 2B
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		return
	}

	// §5.2: if leader’s term >= ours, recognize it as legitimate and become Follower.
	// Only reset votedFor when the term actually advances (not on equal-term messages,
	// so we don’t forget a vote we already cast this term).
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1
	}
	rf.role = Follower

	rf.lastHeartBeat = time.Now()
	reply.Term = rf.currentTerm
	// 2B

	index := args.PrevLogIndex

	if len(rf.log)-1 < index {
		reply.Success = false
		return
	}

	if rf.log[index].Term != args.PrevLogTerm {
		reply.Success = false
		return
	}

	// fix over truncation of uncommited entries
	// handles the heartbeat case with empty args entry

	for i, entry := range args.Entries {
		logIndex := index + 1 + i
		if logIndex < len(rf.log) {
			if rf.log[logIndex].Term != entry.Term {
				rf.log = append(rf.log[:logIndex:logIndex], args.Entries[i:]...)
				break
			}
			continue
		}
		rf.log = append(rf.log, args.Entries[i:]...)
		break
	}
	reply.Success = true

	// leader might have commited entries that's havn't been sent here
	rf.commitIndex = max(min(args.LeaderCommit, len(rf.log)-1), rf.commitIndex)
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm
	isLeader := rf.role == Leader
	if !isLeader {
		return index, term, isLeader
	}
	rf.log = append(rf.log, LogEntry{command, rf.currentTerm})
	index = len(rf.log) - 1
	rf.matchIndex[rf.me] = index
	if len(rf.peers) == 1 {
		rf.commitIndex = index
	}
	return index, term, isLeader
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.me = me

	// Your initialization code here (2A, 2B).
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.lastHeartBeat = time.Now()
	rf.role = Follower
	rf.log = []LogEntry{{nil, 0}} // sentinel at index 0 so lastLogIndex is always >= 0

	// check for timeout
	go func() {
		for {
			if rf.killed() {
				return
			}
			timeout := time.Duration(rand.Intn(150)+150) * time.Millisecond
			time.Sleep(timeout)
			// Check AFTER sleeping so we measure against the same timeout we slept for,
			// avoiding spurious elections when a heartbeat arrived mid-sleep.
			rf.mu.Lock()
			shouldElect := time.Since(rf.lastHeartBeat) > timeout && rf.role != Leader
			if shouldElect {
				rf.role = Candidate
			}
			rf.mu.Unlock()
			if shouldElect {
				rf.startElection()
			}
		}
	}()

	go func() {
		for {

			if rf.killed() {
				return
			}

			rf.mu.Lock()
			role := rf.role
			term := rf.currentTerm
			leaderId := rf.me

			rf.mu.Unlock()

			if role == Leader {

				for i := 0; i < len(rf.peers); i++ {
					if i == leaderId {
						continue
					}
					go func(peerID int) {
						rf.mu.Lock()
						PrevLogIndex := rf.nextIndex[peerID] - 1
						PrevLogTerm := rf.log[PrevLogIndex].Term
						entries := rf.log[PrevLogIndex+1:]
						commitIndex := rf.commitIndex
						rf.mu.Unlock()

						args := AppendEntriesArgs{
							Term:         term,
							LeaderID:     leaderId,
							PrevLogIndex: PrevLogIndex,
							PrevLogTerm:  PrevLogTerm,
							Entries:      entries,
							LeaderCommit: commitIndex,
						}

						var reply AppendEntriesReply
						ok := rf.sendAppendEntries(peerID, &args, &reply)

						if !ok {
							return
						}

						// step down if follower has a higher term
						if reply.Term > term {
							rf.mu.Lock()
							rf.role = Follower
							rf.currentTerm = reply.Term
							rf.votedFor = -1
							rf.mu.Unlock()
							return
						}

						for !reply.Success {
							rf.mu.Lock()
							// Stop retrying if no longer leader or term changed
							if rf.role != Leader || rf.currentTerm != term {
								rf.mu.Unlock()
								return
							}
							if rf.nextIndex[peerID] <= 1 {
								rf.mu.Unlock()
								return
							}
							rf.nextIndex[peerID] -= 1
							PrevLogIndex = rf.nextIndex[peerID] - 1
							PrevLogTerm = rf.log[PrevLogIndex].Term
							entries = rf.log[PrevLogIndex+1:]
							args = AppendEntriesArgs{
								Term:         term,
								LeaderID:     leaderId,
								PrevLogIndex: PrevLogIndex,
								PrevLogTerm:  PrevLogTerm,
								Entries:      entries,
								LeaderCommit: rf.commitIndex,
							}
							rf.mu.Unlock()
							reply = AppendEntriesReply{}
							ok = rf.sendAppendEntries(peerID, &args, &reply)
							if !ok {
								return
							}
							// Check for higher term in retry replies too
							if reply.Term > term {
								rf.mu.Lock()
								rf.role = Follower
								rf.currentTerm = reply.Term
								rf.votedFor = -1
								rf.mu.Unlock()
								return
							}
						}

						// set matchIndex to what the follower actually confirmed,
						confirmedIndex := PrevLogIndex + len(entries)

						rf.mu.Lock()
						defer rf.mu.Unlock()
						// Discard replies from stale terms — matchIndex/nextIndex were
						// reinitialized on re-election, so a stale confirmedIndex could push
						// nextIndex beyond the current log and cause a panic.
						if rf.role != Leader || rf.currentTerm != term {
							return
						}
						// Only advance — never go backwards
						if confirmedIndex > rf.matchIndex[peerID] {
							rf.matchIndex[peerID] = confirmedIndex
							rf.nextIndex[peerID] = confirmedIndex + 1
						}

						majority := len(rf.peers)/2 + 1

						for i := rf.commitIndex + 1; i < len(rf.log); i++ {
							count := 0
							for peer := 0; peer < len(rf.peers); peer++ {
								if rf.matchIndex[peer] >= i {
									count += 1
								}
							}
							// Only commit changes in current term
							if count >= majority && rf.log[i].Term == rf.currentTerm {
								rf.commitIndex = i
							}
						}

					}(i)
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// apply loop
	go func() {
		for {
			if rf.killed() {
				return
			}
			rf.mu.Lock()
			lastApplied := rf.lastApplied
			commitIndex := rf.commitIndex
			log := rf.log
			rf.mu.Unlock()
			if lastApplied < commitIndex {
				var msgs []ApplyMsg
				for i := lastApplied + 1; i <= commitIndex; i++ {
					applyMsg := ApplyMsg{
						true,
						log[i].Command,
						i,
					}
					msgs = append(msgs, applyMsg)
				}

				// Dont' hold the lock, too slow
				for _, msgs := range msgs {
					applyCh <- msgs
				}

				rf.mu.Lock()
				rf.lastApplied = commitIndex
				rf.mu.Unlock()
			}
			time.Sleep(10 * time.Millisecond)

		}
	}()

	return rf
}
