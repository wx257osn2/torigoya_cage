//
// Copyright yutopp 2014 - .
//
// Distributed under the Boost Software License, Version 1.0.
// (See accompanying file LICENSE_1_0.txt or copy at
// http://www.boost.org/LICENSE_1_0.txt)
//

// +build linux

package torigoya

import(
	"errors"
	"fmt"
	"syscall"

	"log"
)


func (bm *ExecutedResult) sendTo(p *BridgePipes) error {
	buf, err := bm.Encode()
	if err != nil { return err }

	p.Result.CloseRead()

	// log.Printf("sendTo :: result => \n", buf)

	n, err := syscall.Write(p.Result.WriteFd, buf)
	if err != nil { return errors.New(fmt.Sprintf("sendTo:: %v", err))  }
	if n != len(buf) { return errors.New(fmt.Sprintf("sendTo:: couldn't write bytes (%d)", n)) }

	log.Printf("sent a result!\n")

	//p.Result.CloseWrite()

	return nil
}
